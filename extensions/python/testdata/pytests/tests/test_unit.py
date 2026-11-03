import unittest


class TestInUnittest(unittest.TestCase):
    def test_ok(self):
        self.assertEqual(1 + 1, 2)

    def test_bad(self):
        self.assertEqual(1 + 1, 3)
