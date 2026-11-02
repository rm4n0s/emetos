"use strict";
function discount(order, rate) {
    const amount = order.total * rate;
    return order.total - amount;
}
const order = { id: 7, customer: "Ada", items: ["pen", "ink"], total: 40 };
const result = discount(order, 0.25);
console.log(`order ${order.id} for ${order.customer}: ${result}`);
//# sourceMappingURL=orders.js.map