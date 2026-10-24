export function lowStock(items, limit) {
  return items.filter((item) => item.quantity < limit);
}
