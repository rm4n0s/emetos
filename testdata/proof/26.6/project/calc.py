"""Prices for a small shop."""


def total(items):
    if not items:
        return 0
    return sum(price * qty for price, qty in items)


def discount(amount, percent):
    if percent > 50:
        raise ValueError("too generous")
    return amount - amount * percent // 100


def label(items):
    if not items:
        return "empty"
    return f"{len(items)} items"


def refund(amount):
    return -amount
