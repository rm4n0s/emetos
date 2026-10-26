// Command fakeclaude is a stand-in for the claude program, for the tests of the Agents extension and for the proofs: it
// speaks the stream-json protocol of SPEC §10.16 from a script instead of asking a model. It is built by the tests
// (extensions/agents/claude/claudetest) and never shipped.
//
// Environment: FAKECLAUDE_SCRIPT is the path of the script, a JSONL file; FAKECLAUDE_LOG, when set, is a JSONL file the
// program appends to (its command line, its process group, its working folder, the variables it got whose names start
// with CLAUDE or ANTHROPIC, and every line it reads); FAKECLAUDE_AUTH is what `fakeclaude auth status` does.
// `fakeclaude auth login` prints a login prompt and reads a line from the terminal: the line "code" ends it with a success
// (and, when FAKECLAUDE_AUTHFILE names a file, writes "in" to it, which makes `auth status` say it is logged in), any other
// line fails it with exit status 1; "hang" never ends. It is what the Log in view of the session tab (step 13.2) runs.
//
// A line of the script is written to the standard output as it is, unless it is an object with a "$" key, which is a
// directive:
//
//	{"$":"wait","for":"user"}                    block until a line of that kind arrives: "user", "control:<subtype>", "control_response" or "any"
//	{"$":"reply","subtype":"x","response":{}}    answer control requests of that subtype with this body from now on
//	{"$":"fail","subtype":"x","error":"text"}    answer control requests of that subtype with an error from now on
//	{"$":"silent","subtype":"x"}                 do not answer control requests of that subtype from now on
//	{"$":"ask","request":{...},"id":"x"}        send a control request to the client and wait for its answer
//	{"$":"replay_user"}                          write the last user line that wait returned back, with "isReplay":true
//	{"$":"raw","line":"text"}                    write the text as a line (for malformed lines)
//	{"$":"large","bytes":n}                      write an assistant message whose text is n bytes long
//	{"$":"sleep","ms":n}                         wait n milliseconds
//	{"$":"stderr","line":"text"}                 write the text as a line of the standard error
//	{"$":"spawn_child","pidfile":"path"}         start a child in this process group and write its PID to the file (FAKECLAUDE_PIDFILE when no path)
//	{"$":"ignore_term"}                          ignore SIGTERM from now on
//	{"$":"hang"}                                 do nothing until killed
//	{"$":"exit","code":n}                        end the program
//
// The directives reply, fail, silent and ignore_term at the start of the script take effect before the first line of the
// input is read, so a script can change the answer to "initialize". Control requests that the script did not say anything about are answered with success; "initialize" gets an empty
// handshake. When the standard input ends the program ends. A hard limit of 90 seconds ends it whatever happens, and a
// child it starts ends after 60.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const hardLimit = 90 * time.Second

func main() {
	time.AfterFunc(hardLimit, func() { os.Exit(3) })
	args := os.Args[1:]
	switch {
	case len(args) > 0 && args[0] == "__child":
		time.Sleep(60 * time.Second)
	case len(args) > 0 && args[0] == "auth":
		auth(args)
	default:
		os.Exit(run(args))
	}
}

// auth answers `claude auth status` as FAKECLAUDE_AUTH says: "in" (the default), "out", "garbage", "crash" or "hang".
func auth(args []string) {
	lg := openLog()
	lg.write(map[string]any{"kind": "auth", "argv": args})
	if len(args) > 1 && args[1] == "login" {
		login(lg)
		return
	}
	if f := os.Getenv("FAKECLAUDE_AUTHFILE"); f != "" {
		if b, err := os.ReadFile(f); err == nil && strings.TrimSpace(string(b)) == "in" {
			fmt.Println(`{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"user@example.invalid"}`)
			return
		}
	}
	switch os.Getenv("FAKECLAUDE_AUTH") {
	case "out":
		fmt.Println(`{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}`)
		os.Exit(1)
	case "garbage":
		fmt.Println("this is not json")
	case "crash":
		fmt.Fprintln(os.Stderr, "fakeclaude: auth store unreadable")
		os.Exit(2)
	case "hang":
		time.Sleep(time.Minute)
	default:
		fmt.Println(`{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty","email":"user@example.invalid","orgName":"Example","subscriptionType":"max","configDirectory":"/nonexistent/config"}`)
	}
}

// login is `fakeclaude auth login`: a prompt on the terminal, then a line from it.
func login(lg *logFile) {
	fmt.Println("Opening the browser to sign in. If it does not open, visit https://login.example.invalid/authorize")
	fmt.Print("Paste the code here > ")
	if os.Getenv("FAKECLAUDE_AUTH") == "hang" {
		time.Sleep(time.Minute)
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	// the code is not logged: only that a line came
	lg.write(map[string]any{"kind": "login-input", "bytes": len(line), "ok": line == "code"})
	if line != "code" {
		fmt.Println("\nLogin failed.")
		os.Exit(1)
	}
	if f := os.Getenv("FAKECLAUDE_AUTHFILE"); f != "" {
		if err := os.WriteFile(f, []byte("in"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude: authfile:", err)
			os.Exit(4)
		}
	}
	fmt.Println("\nLogin successful.")
}

// logFile appends JSONL records to FAKECLAUDE_LOG; with no log it does nothing.
type logFile struct {
	mu sync.Mutex
	f  *os.File
}

func openLog() *logFile {
	p := os.Getenv("FAKECLAUDE_LOG")
	if p == "" {
		return &logFile{}
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: log:", err)
		os.Exit(4)
	}
	return &logFile{f: f}
}

func (l *logFile) write(v map[string]any) {
	if l.f == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.Write(append(b, '\n'))
}

// peer is the state of one run.
type peer struct {
	log   *logFile
	outMu sync.Mutex
	out   *bufio.Writer

	mu      sync.Mutex
	replies map[string]map[string]any // subtype -> success body
	fails   map[string]string
	silent  map[string]bool

	inbox   chan map[string]any
	backlog []map[string]any
	lastUsr map[string]any
	askN    int
}

func (p *peer) writeLine(b []byte) {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	_, _ = p.out.Write(b)
	_ = p.out.WriteByte('\n')
	_ = p.out.Flush()
}

func (p *peer) writeJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: marshal:", err)
		return
	}
	p.writeLine(b)
}

func run(args []string) int {
	p := &peer{
		log: openLog(), out: bufio.NewWriter(os.Stdout),
		replies: map[string]map[string]any{}, fails: map[string]string{}, silent: map[string]bool{},
		inbox: make(chan map[string]any, 1024),
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CLAUDE") || strings.HasPrefix(kv, "ANTHROPIC") {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
	}
	cwd, _ := os.Getwd()
	p.log.write(map[string]any{"kind": "start", "argv": args, "pid": os.Getpid(), "pgid": syscall.Getpgrp(), "cwd": cwd, "env": env})
	script, err := os.ReadFile(os.Getenv("FAKECLAUDE_SCRIPT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeclaude: script:", err)
		return 4
	}
	var readerOnce sync.Once
	sc := bufio.NewScanner(bytes.NewReader(script))
	sc.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if json.Unmarshal(line, &obj) != nil || obj["$"] == nil {
			p.writeLine(line)
			continue
		}
		switch obj["$"] {
		case "reply", "fail", "silent", "ignore_term":
		default:
			readerOnce.Do(func() { go p.readInput() })
		}
		if code, done := p.directive(obj); done {
			return code
		}
	}
	// the script is over: stay until the client ends the conversation
	readerOnce.Do(func() { go p.readInput() })
	for range p.inbox {
	}
	return 0
}

// readInput reads the lines of the standard input, answers the control requests and queues the lines for wait.
func (p *peer) readInput() {
	defer close(p.inbox)
	r := bufio.NewReaderSize(os.Stdin, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var m map[string]any
			if json.Unmarshal(line, &m) == nil {
				p.log.write(map[string]any{"kind": "in", "msg": m})
				if m["type"] == "control_request" {
					p.answer(m)
				}
				p.inbox <- m
			} else {
				p.log.write(map[string]any{"kind": "in-invalid", "line": string(line)})
			}
		}
		if err != nil {
			if err != io.EOF {
				fmt.Fprintln(os.Stderr, "fakeclaude: stdin:", err)
			}
			return
		}
	}
}

// answer replies to a control request of the client.
func (p *peer) answer(m map[string]any) {
	id, _ := m["request_id"].(string)
	req, _ := m["request"].(map[string]any)
	sub, _ := req["subtype"].(string)
	p.mu.Lock()
	body, hasBody := p.replies[sub]
	msg, fails := p.fails[sub]
	silent := p.silent[sub]
	p.mu.Unlock()
	switch {
	case silent:
		return
	case fails:
		p.writeJSON(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "error", "request_id": id, "error": msg}})
		return
	case !hasBody:
		body = map[string]any{}
		if sub == "initialize" {
			body = map[string]any{"commands": []any{}, "models": []any{}, "account": map[string]any{}}
		}
	}
	p.writeJSON(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": id, "response": body}})
}

// next returns the first queued line that match accepts, keeping the others in order; false when the input ended.
func (p *peer) next(match func(map[string]any) bool) (map[string]any, bool) {
	for i, m := range p.backlog {
		if match(m) {
			p.backlog = append(p.backlog[:i], p.backlog[i+1:]...)
			return m, true
		}
	}
	for m := range p.inbox {
		if match(m) {
			return m, true
		}
		p.backlog = append(p.backlog, m)
	}
	return nil, false
}

func matcher(kind string) func(map[string]any) bool {
	return func(m map[string]any) bool {
		switch {
		case kind == "any":
			return true
		case strings.HasPrefix(kind, "control:"):
			req, _ := m["request"].(map[string]any)
			return m["type"] == "control_request" && req["subtype"] == strings.TrimPrefix(kind, "control:")
		default:
			return m["type"] == kind
		}
	}
}

// directive runs one directive of the script; done is true when the program must end with code.
func (p *peer) directive(d map[string]any) (code int, done bool) {
	str := func(k string) string { s, _ := d[k].(string); return s }
	num := func(k string) int { f, _ := d[k].(float64); return int(f) }
	switch str("$") {
	case "wait":
		m, ok := p.next(matcher(str("for")))
		if !ok {
			return 0, true // the client closed the input
		}
		if m["type"] == "user" {
			p.lastUsr = m
		}
	case "reply":
		body, _ := d["response"].(map[string]any)
		p.mu.Lock()
		p.replies[str("subtype")] = body
		p.mu.Unlock()
	case "fail":
		p.mu.Lock()
		p.fails[str("subtype")] = str("error")
		p.mu.Unlock()
	case "silent":
		p.mu.Lock()
		p.silent[str("subtype")] = true
		p.mu.Unlock()
	case "ask":
		id := str("id")
		if id == "" {
			p.askN++
			id = "ask_" + strconv.Itoa(p.askN)
		}
		p.writeJSON(map[string]any{"type": "control_request", "request_id": id, "request": d["request"]})
		_, ok := p.next(func(m map[string]any) bool {
			resp, _ := m["response"].(map[string]any)
			return m["type"] == "control_response" && resp["request_id"] == id
		})
		if !ok {
			return 0, true
		}
	case "replay_user":
		if p.lastUsr != nil {
			m := map[string]any{}
			for k, v := range p.lastUsr {
				m[k] = v
			}
			m["isReplay"] = true
			p.writeJSON(m)
		}
	case "raw":
		p.writeLine([]byte(str("line")))
	case "large":
		p.writeJSON(map[string]any{"type": "assistant", "session_id": "s", "message": map[string]any{
			"model": "m", "content": []any{map[string]any{"type": "text", "text": strings.Repeat("x", num("bytes"))}}}})
	case "sleep":
		time.Sleep(time.Duration(num("ms")) * time.Millisecond)
	case "spawn_child":
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude: executable:", err)
			return 4, true
		}
		child := exec.Command(exe, "__child")
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude: child:", err)
			return 4, true
		}
		pidfile := str("pidfile")
		if pidfile == "" {
			pidfile = os.Getenv("FAKECLAUDE_PIDFILE")
		}
		if err := os.WriteFile(pidfile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "fakeclaude: pidfile:", err)
			return 4, true
		}
	case "stderr":
		fmt.Fprintln(os.Stderr, str("line"))
	case "ignore_term":
		signal.Ignore(syscall.SIGTERM)
	case "hang":
		select {}
	case "exit":
		return num("code"), true
	default:
		fmt.Fprintln(os.Stderr, "fakeclaude: unknown directive", str("$"))
		return 4, true
	}
	return 0, false
}
