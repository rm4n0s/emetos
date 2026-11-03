import { expect, test } from 'vitest';

test('other passes', () => {
  expect(true).toBe(true);
});

test('other crashes', () => {
  throw new Error('boom');
});
