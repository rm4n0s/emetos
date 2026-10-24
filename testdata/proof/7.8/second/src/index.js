import { lowStock } from "./components/stock.js";

const items = [
  { name: "bolts", quantity: 120 },
  { name: "washers", quantity: 4 },
];

for (const item of lowStock(items, 10)) {
  console.log(`low stock: ${item.name} (${item.quantity})`);
}
