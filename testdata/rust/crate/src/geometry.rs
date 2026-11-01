/// A point on the plane.
pub struct Point {
    pub x: f64,
    pub y: f64,
}

/// The point halfway between two points.
pub fn midpoint(a: &Point, b: &Point) -> Point {
    Point { x: (a.x + b.x) / 2.0, y: (a.y + b.y) / 2.0 }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn halfway() {
        let m = midpoint(&Point { x: 0.0, y: 0.0 }, &Point { x: 2.0, y: 4.0 });
        assert_eq!(m.x, 1.0);
    }
}
