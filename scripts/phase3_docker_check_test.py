#!/usr/bin/env python3
"""Synthetic Docker/HTTP checks; no real account, session or daemon required."""
import copy
import http.server
import json
from pathlib import Path
import tempfile
import threading
import unittest

import phase3_docker_check as check


def runtime():
    return {'container_id': 'a' * 64, 'image_id': 'sha256:' + 'b' * 64,
            'started_at': '2026-10-21T01:00:00.000000000Z', 'state': 'running', 'health': 'healthy',
            'user': '65532', 'read_only': True, 'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true'],
            'tmpfs': {'/tmp': 'size=128m,noexec,nosuid'},
            'ports': {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '8084'}]},
            'project': check.PROJECT, 'service': 'drive', 'storage_telegram': True, 'data_in_volume': True,
            'data_mount': {'type': 'volume', 'name': check.PROJECT + '_drive-data', 'writable': True}}


class DockerChecks(unittest.TestCase):
    def test_sanitized_runtime_and_security_rejection(self):
        value = runtime()
        value['credential'] = 'never-copy-this'
        result = check.validate_runtime(value, 8084)
        self.assertNotIn('never-copy-this', json.dumps(result))
        self.assertTrue(all(item == 'PASS' for item in result['checks'].values()))
        for field, wrong in (('project', 'telegram-private-drive'), ('health', 'unhealthy'), ('user', '0'),
                             ('read_only', False), ('cap_drop', []), ('security_opt', []), ('storage_telegram', False),
                             ('data_in_volume', False), ('tmpfs', {'/tmp': 'size=1g,noexec,nosuid'}),
                             ('ports', {'8080/tcp': [{'HostIp': '0.0.0.0', 'HostPort': '8084'}]}),
                             ('data_mount', {'type': 'bind', 'name': 'preexisting', 'writable': True})):
            with self.subTest(field=field), self.assertRaises(check.QualificationError):
                bad = copy.deepcopy(value)
                bad[field] = wrong
                check.validate_runtime(bad, 8084)

    def test_restart_and_recreation_keep_same_volume(self):
        previous = {'project': check.PROJECT, 'runtime': check.validate_runtime(runtime(), 8084)}
        restarted = copy.deepcopy(previous['runtime'])
        restarted['started_at'] = '2026-10-21T01:01:00Z'
        check.verify_transition(restarted, previous, 'restart')
        recreated = copy.deepcopy(restarted)
        recreated['container_id'] = 'c' * 64
        check.verify_transition(recreated, previous, 'recreate')
        for current, transition in ((previous['runtime'], 'restart'), (previous['runtime'], 'recreate'), (recreated, 'restart')):
            with self.subTest(transition=transition), self.assertRaises(check.QualificationError):
                check.verify_transition(current, previous, transition)
        previous['runtime']['volume'] = 'original-drive-data'
        with self.assertRaises(check.QualificationError):
            check.verify_transition(recreated, previous, 'recreate')

    def test_password_permissions_liveness_readiness_and_logout(self):
        class Handler(http.server.BaseHTTPRequestHandler):
            connected = True
            logged_out = 0

            def log_message(self, *args):
                pass

            def do_GET(self):
                if self.path == '/health':
                    body, status = {'status': 'ok'}, 200
                elif self.headers.get('Cookie') != 'drive_session=synthetic':
                    body, status = {'error': 'unauthorized'}, 401
                else:
                    body, status = {'adapter': 'telegram', 'database': 'ok', 'encryption': 'AES-256-GCM',
                                    'storage_connection': {'state': 'connected' if self.connected else 'disconnected',
                                                           'retry_count': 3, 'private_identity': 'never-save-this'},
                                    'chunk_size': 4 << 20}, 200
                self.send_response(status)
                self.end_headers()
                self.wfile.write((json.dumps(body, separators=(',', ':')) + '\n').encode())

            def do_POST(self):
                self.rfile.read(int(self.headers.get('Content-Length', 0)))
                if self.path == '/api/v1/auth/login':
                    self.send_response(200)
                    self.send_header('Set-Cookie', 'drive_session=synthetic; HttpOnly; SameSite=Strict; Path=/')
                    self.end_headers()
                    self.wfile.write(b'{"authenticated":true}')
                else:
                    Handler.logged_out += 1
                    self.send_response(204)
                    self.end_headers()

        with tempfile.TemporaryDirectory() as directory:
            password = Path(directory) / 'owner-password'
            password.write_text('synthetic-password')
            password.chmod(0o600)
            with http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler) as server:
                worker = threading.Thread(target=server.serve_forever)
                worker.start()
                try:
                    origin = 'http://127.0.0.1:' + str(server.server_port)
                    observed = check.readiness(origin, password)
                    self.assertEqual(observed['state'], 'connected')
                    self.assertEqual(observed['retry_count'], 3)
                    self.assertNotIn('never-save-this', json.dumps(observed))
                    self.assertEqual(Handler.logged_out, 1)
                    Handler.connected = False
                    with self.assertRaises(check.QualificationError):
                        check.readiness(origin, password)
                    self.assertEqual(Handler.logged_out, 2)
                    password.chmod(0o644)
                    with self.assertRaises(check.QualificationError):
                        check.readiness(origin, password)
                    linked = Path(directory) / 'linked-password'
                    linked.symlink_to(password)
                    with self.assertRaises(check.QualificationError):
                        check.readiness(origin, linked)
                finally:
                    server.shutdown()
                    worker.join()


if __name__ == '__main__':
    unittest.main()
