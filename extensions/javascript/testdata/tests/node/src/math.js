function add(a, b) {
  return a + b;
}

function divide(a, b) {
  if (b === 0) {
    throw new Error('division by zero');
  }
  return a / b;
}

function unused() {
  return 'never called';
}

module.exports = { add, divide, unused };
