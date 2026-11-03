import pytest
from calc import double, sign


def test_double():
    assert double(2) == 4


def test_double_wrong():
    assert double(2) == 5


@pytest.mark.parametrize("n,expected", [(3, 1), (-3, -1), (0, 0), (5, -1)])
def test_sign(n, expected):
    assert sign(n) == expected


@pytest.mark.skip(reason="not today")
def test_skipped():
    pass


@pytest.mark.xfail(reason="known bug")
def test_known_bug():
    assert double(1) == 3


class TestSign:
    def test_positive(self):
        assert sign(10) == 1

    def test_raises(self):
        with pytest.raises(TypeError):
            sign("x")
