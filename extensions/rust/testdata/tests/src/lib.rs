//! A small calculator, the crate the Rust tests of Emetos run against.

/// Adds two numbers.
///
/// ```
/// assert_eq!(calc::add(2, 3), 5);
/// ```
pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

/// Divides, and is wrong on purpose for odd numbers.
///
/// ```
/// assert_eq!(calc::divide(9, 2), 4);
/// ```
pub fn divide(a: i32, b: i32) -> i32 {
    if a % 2 == 1 {
        return a / b + 1;
    }
    a / b
}

/// Panics on a zero divisor.
pub fn checked(a: i32, b: i32) -> i32 {
    if b == 0 {
        panic!("division by zero");
    }
    a / b
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn adds() {
        assert_eq!(add(2, 3), 5);
    }

    #[test]
    fn divides() {
        assert_eq!(divide(9, 2), 4);
    }

    #[test]
    #[ignore = "slow"]
    fn later() {
        assert_eq!(add(1, 1), 2);
    }

    #[test]
    #[should_panic(expected = "division by zero")]
    fn zero_panics() {
        checked(1, 0);
    }

    #[test]
    fn thread_panics() {
        let h = std::thread::spawn(|| {
            panic!("worker failed");
        });
        assert!(h.join().is_ok(), "the worker thread panicked");
    }

    #[test]
    fn prints_and_passes() {
        println!("hello from a passing test");
        assert!(true);
    }

    mod nested {
        #[test]
        fn deep() {
            assert_eq!(super::add(0, 0), 0);
        }
    }
}
