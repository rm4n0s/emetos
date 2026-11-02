// A TypeScript program for the tests of the JavaScript debugger (tag emetos_jsdebug). orders.js and orders.js.map are made from it with
// the pinned TypeScript (see the entry of step 25.10 in docs/IMPLEMENTED.md); the tests need no tsc.
interface Order {
  id: number;
  total: number;
  items: string[];
}

function discount(order: Order, rate: number): number {
  const amount: number = order.total * rate;
  return order.total - amount;
}

const order: Order = { id: 7, total: 40, items: ["pen", "ink"] };
const result: number = discount(order, 0.25);
console.log("result", result);
