import { describe, expect, it } from 'vitest';
import { discount, label } from '../src/pricing';

describe('pricing', () => {
  it('takes a percentage off', () => {
    expect(discount(200, 25)).toBe(150);
  });

  it('labels a bulk order', () => {
    expect(label(2500)).toBe('bulk');
  });

  it('labels a regular order', () => {
    expect(label(40)).toBe('normal');
  });
});
