// The orders of the MongoDB shot of README.md (mongosh, run in the labelled test container by scripts/screenshots.sh).
db = db.getSiblingDB("shop");
db.orders.drop();
db.orders.insertMany([
  {sku: "A1", qty: 2, price: 9.5, paid: true, created: new Date("2026-10-29T09:30:00Z"), customer: {name: "Ann", address: {city: "Athens"}}, tags: ["new", "gift"]},
  {sku: "B2", qty: 5, price: 20.0, paid: false, created: new Date("2026-10-28T09:30:00Z"), customer: {name: "Bob"}},
  {sku: "C3", qty: 1, price: 4.25, paid: true, created: new Date("2026-10-27T09:30:00Z")}
]);
db.customers.insertMany([{name: "Ann", city: "Athens"}, {name: "Bob", city: "Berlin"}]);
db.orders.createIndex({sku: 1}, {unique: true});
