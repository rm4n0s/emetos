"use strict";
function discount(order, rate) {
    const amount = order.total * rate;
    return order.total - amount;
}
const order = { id: 7, total: 40, items: ["pen", "ink"] };
const result = discount(order, 0.25);
console.log("result", result);
//# sourceMappingURL=orders.js.map