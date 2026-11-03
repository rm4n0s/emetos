export function discount(total: number, percent: number): number {
  if (percent < 0 || percent > 100) {
    throw new RangeError('percent must be between 0 and 100');
  }
  return Math.round(total * (100 - percent)) / 100;
}

export function label(total: number): string {
  if (total === 0) {
    return 'empty';
  }
  return total > 1000 ? 'bulk' : 'regular';
}
