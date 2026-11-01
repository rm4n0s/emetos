mod geometry;
use geometry::{midpoint, Point};
macro_rules! double { ($x:expr) => { $x * 2.0 }; }

fn main() {
    let a = Point { x: 1.0, y: 2.0 };
    let middle = midpoint(&a, &Point { x: 3.0, y: 4.0 });

    let doubled = double!(middle.x);
    let _ = (doubled, middle.y);
}
