import { describe, expect, it } from 'vitest';
import { Inventory } from '../src/inventory';

describe('Inventory', () => {
  it('adds items and sums their value', () => {
    const inventory = new Inventory();
    inventory.add({ sku: 'a', name: 'Bolt', quantity: 10, price: 2 });
    inventory.add({ sku: 'b', name: 'Nut', quantity: 5, price: 3 });
    expect(inventory.total()).toBe(35);
  });

  it('merges the same sku', () => {
    const inventory = new Inventory();
    inventory.add({ sku: 'a', name: 'Bolt', quantity: 10, price: 2 });
    inventory.add({ sku: 'a', name: 'Bolt', quantity: 5, price: 2 });
    expect(inventory.report()).toEqual(['Bolt: 15']);
  });

  it('removes items', () => {
    const inventory = new Inventory();
    inventory.add({ sku: 'a', name: 'Bolt', quantity: 10, price: 2 });
    inventory.remove('a', 4);
    expect(inventory.report()).toEqual(['Bolt: 5']);
  });

  it('refuses to remove more than there is', () => {
    const inventory = new Inventory();
    inventory.add({ sku: 'a', name: 'Bolt', quantity: 1, price: 2 });
    expect(() => inventory.remove('a', 2)).toThrow(/only 1 of a left/);
  });

  it.skip('persists to disk', () => {});

  it.each([
    [1, 2],
    [3, 6],
  ])('values %i bolts at %i', (quantity, total) => {
    const inventory = new Inventory();
    inventory.add({ sku: 'a', name: 'Bolt', quantity, price: 2 });
    expect(inventory.total()).toBe(total);
  });
});
