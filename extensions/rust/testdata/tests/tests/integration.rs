#[test]
fn adds_across_the_crate() {
    assert_eq!(calc::add(40, 2), 42);
}

#[test]
fn checked_divides() {
    assert_eq!(calc::checked(8, 2), 5);
}
