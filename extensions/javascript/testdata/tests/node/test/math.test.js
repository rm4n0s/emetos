const { describe, it, test } = require('node:test');
const assert = require('node:assert');
const { add, divide } = require('../src/math');

describe('math', () => {
  it('adds', () => {
    assert.strictEqual(add(2, 3), 5);
  });

  it('divides', () => {
    assert.strictEqual(divide(10, 2), 4);
  });

  it.skip('later', () => {});

  describe('nested', () => {
    it('throws on zero', () => {
      assert.throws(() => divide(1, 0), /zero/);
    });
  });
});

test('prints and passes', () => {
  console.log('hello from a passing test');
});
