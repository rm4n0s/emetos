export interface Item {
  sku: string;
  name: string;
  quantity: number;
  price: number;
}

export class Inventory {
  private items = new Map<string, Item>();

  add(item: Item): void {
    const existing = this.items.get(item.sku);
    if (existing) {
      existing.quantity += item.quantity;
      return;
    }
    this.items.set(item.sku, { ...item });
  }

  remove(sku: string, quantity: number): void {
    const item = this.items.get(sku);
    if (!item) {
      throw new Error(`unknown sku ${sku}`);
    }
    if (item.quantity < quantity) {
      throw new Error(`only ${item.quantity} of ${sku} left`);
    }
    item.quantity -= quantity;
  }

  total(): number {
    let sum = 0;
    for (const item of this.items.values()) {
      sum += item.quantity * item.price;
    }
    return sum;
  }

  report(): string[] {
    return [...this.items.values()].map((i) => `${i.name}: ${i.quantity}`);
  }
}
