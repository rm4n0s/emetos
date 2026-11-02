struct Order {
    id: u32,
    items: Vec<String>,
    total: f64,
}

fn discount(order: &Order, rate: f64) -> f64 {
    let amount = order.total * rate;
    let rounded = (amount * 100.0).round() / 100.0;
    rounded
}

fn main() {
    let order = Order { id: 7, items: vec!["keyboard".to_string(), "mouse".to_string()], total: 49.5 };
    let saved = discount(&order, 0.1);
    println!("order {} ({} items) saves {:.2}", order.id, order.items.len(), saved);
}
