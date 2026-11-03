import assert from 'node:assert';
import { describe, it } from 'node:test';
import { describeShape, square } from '../src/shapes.ts';

describe('shapes', () => {
  it('computes an area', () => {
    assert.strictEqual(square(3).area(), 9);
  });

  it('describes a small square', () => {
    assert.strictEqual(describeShape(square(2)), 'a small square');
  });

  it('describes a large square', () => {
    assert.strictEqual(describeShape(square(11)), 'a big square');
  });

  it.skip('draws a square', () => {});
});
