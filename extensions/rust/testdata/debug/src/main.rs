use std::env;

fn square(x: i32) -> i32 {
    let result = x * x;
    let doubled = result + result;
    doubled - result
}

fn main() {
    let n: i32 = env::args().nth(1).and_then(|a| a.parse().ok()).unwrap_or(4);
    let mut total = 0;
    for i in 1..=n {
        total += square(i);
    }
    println!("total {}", total);
}

#[cfg(test)]
mod tests {
    use super::square;

    #[test]
    fn squares() {
        assert_eq!(square(3), 9);
    }
}
