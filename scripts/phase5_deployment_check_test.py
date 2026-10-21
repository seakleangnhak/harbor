#!/usr/bin/env python3
"""Synthetic guards for isolated deployment qualification; no daemon needed."""
import copy
import json
import unittest

import phase5_deployment_check as check


def runtime():
    return {'id': 'a' * 64, 'image': 'sha256:' + 'b' * 64, 'started_at': '2026-10-21T03:00:00Z',
            'project': check.PROJECT, 'service': 'drive', 'local': True, 'allow_remote_http': True,
            'running': True, 'health': 'healthy', 'user': '65532', 'read_only': True,
            'privileged': False, 'restart_policy': 'unless-stopped', 'stop_timeout': 20,
            'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true'],
            'memory': 768 << 20, 'cpus': 2_000_000_000, 'pids': 128,
            'ports': {'8080/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '8092'}]},
            'tmpfs': {'/tmp': 'size=128m,noexec,nosuid,nodev'},
            'data_mount': {'type': 'volume', 'name': check.VOLUME, 'writable': True}}


class DeploymentGuards(unittest.TestCase):
    def test_sanitized_runtime(self):
        value = runtime()
        value['unrelated_private_value'] = 'never-save-this'
        observed = check.validate_runtime(value, 8092)
        self.assertNotIn('never-save-this', json.dumps(observed))
        self.assertTrue(all(v == 'PASS' for v in observed['checks'].values()))

    def test_insecure_or_unisolated_runtime_rejected(self):
        for field, value in (('project', 'telegram-private-drive'), ('local', False), ('user', '0'),
                             ('health', 'unhealthy'), ('running', False), ('read_only', False),
                             ('privileged', True), ('restart_policy', 'no'), ('stop_timeout', 1),
                             ('cap_drop', []), ('security_opt', []), ('allow_remote_http', False),
                             ('memory', 0), ('cpus', 0), ('pids', 0), ('tmpfs', {}),
                             ('ports', {'8080/tcp': [{'HostIp': '0.0.0.0', 'HostPort': '8092'}]}),
                             ('data_mount', {'type': 'volume', 'name': 'original', 'writable': True}),
                             ('id', 'invalid'), ('image', 'invalid')):
            with self.subTest(field=field), self.assertRaises(check.QualificationError):
                bad = copy.deepcopy(runtime())
                bad[field] = value
                check.validate_runtime(bad, 8092)

    def test_each_transition_observed_and_volume_unchanged(self):
        old = check.validate_runtime(runtime(), 8092)
        current = dict(old, started_at='2026-10-21T03:01:00Z')
        check.check_transition(current, old, 'restart')
        recreated = dict(current, id='c' * 64)
        check.check_transition(recreated, old, 'recreate')
        upgraded = dict(recreated, image='sha256:' + 'd' * 64)
        check.check_transition(upgraded, old, 'upgrade')
        check.check_transition(upgraded, old, 'rollback')
        for new, label in ((old, 'restart'), (old, 'recreate'), (upgraded, 'recreate'), (recreated, 'upgrade'), (recreated, 'rollback'),
                           (dict(upgraded, volume='original'), 'upgrade')):
            with self.subTest(label=label), self.assertRaises(check.QualificationError):
                check.check_transition(new, old, label)


if __name__ == '__main__':
    unittest.main()
