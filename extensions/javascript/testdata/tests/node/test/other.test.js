const test = require('node:test');
const assert = require('node:assert');

test('other passes', () => {
  assert.ok(true);
});

test('other crashes', () => {
  throw new Error('boom');
});
