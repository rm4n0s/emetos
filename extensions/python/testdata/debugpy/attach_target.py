import time


def work(round_number):
    value = round_number * 2
    return value


for r in range(1, 2000):
    work(r)
    time.sleep(0.05)
