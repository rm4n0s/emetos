import pytest

from calc import discount, label, total


@pytest.mark.parametrize("items,expected", [([], 0), ([(5, 2)], 10), ([(5, 2), (3, 1)], 13)])
def test_total(items, expected):
    assert total(items) == expected


def test_discount():
    assert discount(200, 10) == 175


def test_label():
    assert label([(1, 1)]) == "1 item"


@pytest.mark.skip(reason="later")
def test_later():
    pass


class TestLimits:
    def test_too_generous(self):
        with pytest.raises(ValueError):
            discount(100, 60)
