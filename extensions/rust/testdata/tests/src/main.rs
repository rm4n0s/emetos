fn main() {
    println!("{}", calc::add(1, 2));
}

#[cfg(test)]
mod tests {
    #[test]
    fn main_runs() {
        assert_eq!(calc::add(1, 2), 3);
    }
}
