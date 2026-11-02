// A small TypeScript program for the proof of step 25.10. out/orders.js and out/orders.js.map are made from it with the pinned TypeScript
// (the command is in the entry of step 25.10 of docs/IMPLEMENTED.md).
interface Order {
  id: number;
  customer: string;
  items: string[];
  total: number;
}

function discount(order: Order, rate: number): number {
  const amount: number = order.total * rate;
  return order.total - amount;
}

const order: Order = { id: 7, customer: "Ada", items: ["pen", "ink"], total: 40 };
const result: number = discount(order, 0.25);
console.log(`order ${order.id} for ${order.customer}: ${result}`);
