const { add, divide } = require('./math');

describe('math', () => {
  test('adds', () => {
    expect(add(2, 3)).toBe(5);
  });

  test('divides', () => {
    expect(divide(10, 2)).toBe(4);
  });

  test.skip('later', () => {});

  describe('nested', () => {
    it('throws on zero', () => {
      expect(() => divide(1, 0)).toThrow('zero');
    });
  });

  test.each([
    [1, 1, 2],
    [2, 3, 5],
  ])('adds %i and %i', (a, b, sum) => {
    expect(add(a, b)).toBe(sum);
  });
});

test('prints and passes', () => {
  console.log('hello from a passing test');
});
