import pytest


@pytest.fixture
def broken():
    raise RuntimeError("the fixture failed")


def test_uses_broken(broken):
    pass


def test_prints():
    print("hello from the test")
    assert True
