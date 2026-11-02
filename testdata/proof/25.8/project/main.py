from dataclasses import dataclass


@dataclass
class Order:
    id: int
    items: list
    total: float


def discount(order, rate):
    amount = order.total * rate
    return amount


if __name__ == "__main__":
    order = Order(7, ["keyboard", "mouse"], 49.5)
    saved = discount(order, 0.1)
    print(f"order {order.id} saves {saved:.2f}")
