def fail(depth):
    if depth == 0:
        raise ValueError("boom at depth 0")
    fail(depth - 1)


print("starting")
fail(2)
