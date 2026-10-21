import hashlib
import unittest
from phase5_copy_check import BLOCK, fixture_chunk, fixture_hash


class CopyFixtureTests(unittest.TestCase):
    def test_unaligned_chunking_reconstructs_identical_bytes(self):
        size = len(BLOCK) + 31
        split = 65539
        pieces = [fixture_chunk(offset, min(split, size - offset)) for offset in range(0, size, split)]
        self.assertEqual(hashlib.sha256(b''.join(pieces)).hexdigest(), fixture_hash(size))
        self.assertEqual(fixture_chunk(0, size), b''.join(pieces))

    def test_empty_fixture_hash(self):
        self.assertEqual(fixture_hash(0), hashlib.sha256(b'').hexdigest())


if __name__ == '__main__':
    unittest.main()
