import sys


def compute(n):
    squares = [i * i for i in range(1, n + 1)]
    total = sum(squares)
    return total


if __name__ == "__main__":
    n = int(sys.argv[1]) if len(sys.argv) > 1 else 5
    config = {"name": "hello", "values": [1, 2, 3]}
    result = compute(n)
    print("result", result)
