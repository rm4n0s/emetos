export interface Shape {
  kind: string;
  area(): number;
}

export function square(side: number): Shape {
  return { kind: 'square', area: () => side * side };
}

export function describeShape(shape: Shape): string {
  if (shape.area() > 100) {
    return `a large ${shape.kind}`;
  }
  return `a small ${shape.kind}`;
}

export function unused(): string {
  return 'never called';
}
