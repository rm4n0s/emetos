import { lowStock } from "../src/components/stock.js";

console.assert(lowStock([{ name: "a", quantity: 1 }], 2).length === 1);
