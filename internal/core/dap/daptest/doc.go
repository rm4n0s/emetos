// Package daptest is the test double of a debug adapter (SPEC §10.31 "Tests", D43): a fake adapter that follows a script, a
// harness that connects a dap.Client to it, and the program (fakeadapter) that the harness starts as a real process in its own
// process group.
//
// A script is a list of steps, written in Go with the constructors of this package or as JSON lines in a file: expect a request
// (and check its arguments), answer it, send events, send a request of the adapter and wait for its answer, delay, print
// garbage, crash, stall, send a burst of events. A test describes a whole session with it. The adapter records every message
// it gets and every protocol violation it notices (a sequence number that does not go up by one, a request that is not the
// one the script expects), which the test reads with Traffic and Violations, or, from the other side of a process, through the
// requests "$daptest/traffic" and "$daptest/violations".
//
// The adapter reads and writes the base protocol with dap.ReadFrame and dap.WriteFrame, but builds its messages itself, so a
// wrong message of the client shows as a violation and not as a shared bug.
//
// A package whose tests need the program calls Main from its TestMain, which builds the program once into a folder of its
// own; Program finds it (and builds it into the test's folder when TestMain did not).
package daptest
