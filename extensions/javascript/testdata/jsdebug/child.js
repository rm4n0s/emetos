// The child process of parent.js (tag emetos_jsdebug).
function greet(who) {
  const message = "hello " + who;
  return message;
}

console.log(greet(process.argv[2]));
