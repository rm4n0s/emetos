// Starts child.js as a child process (tag emetos_jsdebug): the debugger gives the child a session of its own.
const { spawn } = require("child_process");
const path = require("path");

const child = spawn(process.execPath, [path.join(__dirname, "child.js"), "world"], { stdio: "inherit" });
child.on("exit", (code) => {
  console.log("child exited with", code);
});
