package daptest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"

	"github.com/rm4n0s/errors"
)

// The operations of a step.
const (
	// OpExpect waits for the next request of the client, checks that it is Command and that its arguments contain Args, and
	// remembers it under As (Command when As is empty). A request that is not the expected one is a violation, is answered with
	// an error, and does not consume the step.
	OpExpect = "expect"
	// OpRespond answers the request remembered under To (the last one when To is empty).
	OpRespond = "respond"
	// OpReply is OpExpect and OpRespond in one step.
	OpReply = "reply"
	// OpCollect waits until a request of each of Commands has arrived, in any order, and remembers each under its command.
	OpCollect = "collect"
	// OpEvent sends an event.
	OpEvent = "event"
	// OpRequest sends a request of the adapter to the client, and remembers its number under As.
	OpRequest = "request"
	// OpAwait waits for the response of the request sent under As; the response is recorded in the traffic.
	OpAwait = "await"
	// OpDelay waits Ms milliseconds.
	OpDelay = "delay"
	// OpBurst sends Count events of the name Event (output when empty), each with a body of about Bytes bytes and its number.
	OpBurst = "burst"
	// OpSend sends the JSON of Body as a message, with the framing; Text, when set, is sent as the body instead (invalid JSON).
	OpSend = "send"
	// OpRaw writes Text to the stream as it is, with no framing (garbage, a bad header, half a message).
	OpRaw = "raw"
	// OpStderr writes Text and a line break to the standard error of the program.
	OpStderr = "stderr"
	// OpCrash ends the adapter at once with the exit status Code and no flush; in a process it is os.Exit.
	OpCrash = "crash"
	// OpExit closes the stream and ends the script; the program exits with the status Code.
	OpExit = "exit"
	// OpStall stops answering: it reads and ignores everything until the stream ends.
	OpStall = "stall"
	// OpWaitClose waits until the client closes the stream.
	OpWaitClose = "waitClose"
)

// Step is one step of a script; which members matter depends on Op.
type Step struct {
	Op string `json:"op"`
	// Command is the command of the request to expect, answer or send.
	Command string `json:"command,omitempty"`
	// Commands are the commands OpCollect waits for.
	Commands []string `json:"commands,omitempty"`
	// Args are the arguments the expected request must contain: every member of Args must be in the arguments, with an equal
	// value (objects are compared member by member, arrays and scalars whole).
	Args json.RawMessage `json:"args,omitempty"`
	// As names the request for OpRespond and OpAwait.
	As string `json:"as,omitempty"`
	// To names the request OpRespond answers.
	To string `json:"to,omitempty"`
	// Body is the body of the response or the event, or the message of OpSend.
	Body json.RawMessage `json:"body,omitempty"`
	// Fail makes the response unsuccessful, with Message as its message.
	Fail bool `json:"fail,omitempty"`
	// Message is the message of an unsuccessful response.
	Message string `json:"message,omitempty"`
	// Event is the name of the event.
	Event string `json:"event,omitempty"`
	// Arguments are the arguments of the request OpRequest sends.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Ms is the delay: of OpDelay, or before an answer of OpReply and OpRespond.
	Ms int `json:"ms,omitempty"`
	// Count and Bytes shape OpBurst.
	Count int `json:"count,omitempty"`
	Bytes int `json:"bytes,omitempty"`
	// Text is what OpRaw, OpSend and OpStderr write.
	Text string `json:"text,omitempty"`
	// Code is the exit status of OpCrash and OpExit.
	Code int `json:"code,omitempty"`
}

// Script is what the fake adapter does, in order. After the last step the adapter answers every request with an error and notes
// a violation, until the stream ends.
type Script []Step

// Lines is the script as JSON lines, one step on each line (the format of the file the fake adapter program reads).
func (s Script) Lines() ([]byte, error) {
	var out bytes.Buffer
	for i, st := range s {
		line, err := json.Marshal(st)
		if err != nil {
			return nil, errors.NewErr("ScriptInvalid", err).SetMetadata("step", i)
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

// ParseScript reads a script from JSON lines; empty lines and lines that start with # are skipped. It fails with the tag
// ScriptInvalid, with the line number.
func ParseScript(data []byte) (Script, error) {
	var s Script
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 64<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var st Step
		if err := json.Unmarshal([]byte(line), &st); err != nil {
			return nil, errors.NewErr("ScriptInvalid", err).SetMetadata("line", n)
		}
		if st.Op == "" {
			return nil, errors.New("ScriptInvalid", "a step has no op", "line", n)
		}
		s = append(s, st)
	}
	if err := sc.Err(); err != nil {
		return nil, errors.NewErr("ScriptInvalid", err)
	}
	return s, nil
}

// LoadScript reads a script from a file of JSON lines. It fails with the tags ScriptReadFailed and ScriptInvalid.
func LoadScript(path string) (Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.NewErr("ScriptReadFailed", err).SetMetadata("path", path)
	}
	return ParseScript(data)
}

// JSON encodes v for a step. It panics when v cannot be encoded, which is a mistake in the test, not a condition to handle.
func JSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("daptest.JSON: " + err.Error())
	}
	return b
}

// Expect is a step that waits for a request of command whose arguments contain args (nil: any).
func Expect(command string, args any) Step {
	st := Step{Op: OpExpect, Command: command}
	if args != nil {
		st.Args = JSON(args)
	}
	return st
}

// Respond is a step that answers the request remembered under to with body.
func Respond(to string, body any) Step {
	st := Step{Op: OpRespond, To: to}
	if body != nil {
		st.Body = JSON(body)
	}
	return st
}

// RespondError is a step that answers the request remembered under to with an error.
func RespondError(to, message string, body any) Step {
	st := Step{Op: OpRespond, To: to, Fail: true, Message: message}
	if body != nil {
		st.Body = JSON(body)
	}
	return st
}

// Reply is a step that waits for a request of command and answers it with body.
func Reply(command string, body any) Step {
	st := Step{Op: OpReply, Command: command}
	if body != nil {
		st.Body = JSON(body)
	}
	return st
}

// ReplyArgs is Reply that also checks the arguments of the request.
func ReplyArgs(command string, args, body any) Step {
	st := Reply(command, body)
	if args != nil {
		st.Args = JSON(args)
	}
	return st
}

// ReplyError is a step that waits for a request of command and answers it with an error.
func ReplyError(command, message string, body any) Step {
	st := Reply(command, body)
	st.Fail, st.Message = true, message
	return st
}

// Collect is a step that waits for a request of each command, in any order.
func Collect(commands ...string) Step { return Step{Op: OpCollect, Commands: commands} }

// Event is a step that sends an event.
func Event(name string, body any) Step {
	st := Step{Op: OpEvent, Event: name}
	if body != nil {
		st.Body = JSON(body)
	}
	return st
}

// Request is a step that sends a request of the adapter, remembered under as.
func Request(as, command string, arguments any) Step {
	st := Step{Op: OpRequest, As: as, Command: command}
	if arguments != nil {
		st.Arguments = JSON(arguments)
	}
	return st
}

// Await is a step that waits for the response to the request sent under as.
func Await(as string) Step { return Step{Op: OpAwait, As: as} }

// Delay is a step that waits for ms milliseconds.
func Delay(ms int) Step { return Step{Op: OpDelay, Ms: ms} }

// Burst is a step that sends count output events of about bytes bytes each.
func Burst(count, bytes int) Step { return Step{Op: OpBurst, Count: count, Bytes: bytes} }

// Send is a step that sends body as a message.
func Send(body any) Step { return Step{Op: OpSend, Body: JSON(body)} }

// SendText is a step that sends text as the body of a message, whatever it is.
func SendText(text string) Step { return Step{Op: OpSend, Text: text} }

// Raw is a step that writes text to the stream as it is.
func Raw(text string) Step { return Step{Op: OpRaw, Text: text} }

// Stderr is a step that writes a line to the standard error of the program.
func Stderr(text string) Step { return Step{Op: OpStderr, Text: text} }

// Crash is a step that ends the adapter at once with the exit status code.
func Crash(code int) Step { return Step{Op: OpCrash, Code: code} }

// Exit is a step that closes the stream and ends the script.
func Exit(code int) Step { return Step{Op: OpExit, Code: code} }

// Stall is a step after which the adapter answers nothing.
func Stall() Step { return Step{Op: OpStall} }

// WaitClose is a step that waits until the client closes the stream.
func WaitClose() Step { return Step{Op: OpWaitClose} }

// After returns the step with a delay of ms milliseconds before its answer (OpReply, OpRespond).
func (s Step) After(ms int) Step {
	s.Ms = ms
	return s
}
