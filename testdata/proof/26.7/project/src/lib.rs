//! Small shape helpers.

/// The area of a rectangle.
///
/// ```
/// assert_eq!(shapes::area(3, 4), 12);
/// ```
pub fn area(w: u32, h: u32) -> u32 {
    w * h
}

/// The perimeter of a rectangle (the sum is wrong on purpose).
pub fn perimeter(w: u32, h: u32) -> u32 {
    2 * w + h
}

/// A description of a rectangle.
pub fn describe(w: u32, h: u32) -> String {
    format!("{}x{}\narea {}", w, h, area(w, h))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn area_of_a_rectangle() {
        assert_eq!(area(3, 4), 12);
    }

    #[test]
    fn perimeter_of_a_rectangle() {
        assert_eq!(perimeter(3, 4), 14);
    }

    #[test]
    fn description_has_the_area() {
        assert_eq!(describe(3, 4), "3x4\narea 14");
    }

    #[test]
    #[ignore = "needs a network"]
    fn download() {}

    #[test]
    #[should_panic(expected = "overflow")]
    fn huge_area_overflows() {
        area(u32::MAX, 2);
    }

    mod squares {
        use super::*;

        #[test]
        fn a_square_is_a_rectangle() {
            assert_eq!(area(5, 5), 25);
        }
    }
}
