import unittest

from calc import double, sign


class TestDouble(unittest.TestCase):
    def test_ok(self):
        self.assertEqual(double(2), 4)

    def test_wrong(self):
        self.assertEqual(double(2), 5)

    def test_error(self):
        raise ValueError("boom")

    @unittest.skip("not today")
    def test_skipped(self):
        pass

    def test_subtests(self):
        for n in (1, 2, 3):
            with self.subTest(n=n):
                self.assertEqual(sign(n), 0 if n == 2 else 1)


class TestSetup(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        raise RuntimeError("no database")

    def test_never(self):
        pass
