package dap

import (
	"context"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// DefaultServerTimeout is how long StartServer waits for the program to print its port.
const DefaultServerTimeout = 30 * time.Second

// loopback is the host of a server that gives none, or only an address that means "every interface".
const loopback = "127.0.0.1"

// DefaultPortPattern finds the address a server program prints, such as "DAP server listening at: 127.0.0.1:38000" (dlv) or
// "Debug server listening at 127.0.0.1:8123" (js-debug): the last words of a line that end in an optional host and a port. A
// pattern of ServerSpec needs the named groups "port" and, optionally, "host".
const DefaultPortPattern = `(?:(?P<host>\[[0-9a-fA-F:]+\]|[A-Za-z0-9._-]+):)?(?P<port>[0-9]{1,5})\s*$`

// DialTCP connects to a debug adapter that listens on a TCP port. An empty host is 127.0.0.1. It fails with the tag
// DapAdapterConnectFailed.
func DialTCP(ctx context.Context, host string, port int) (net.Conn, error) {
	if host == "" {
		host = loopback
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, errors.NewErr("DapAdapterConnectFailed", err).SetMetadata("address", addr)
	}
	return conn, nil
}

// DialUnix connects to a debug adapter that listens on a Unix socket. It fails with the tag DapAdapterConnectFailed.
func DialUnix(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, errors.NewErr("DapAdapterConnectFailed", err).SetMetadata("address", path)
	}
	return conn, nil
}

// ServerSpec says how StartServer finds the port of a server program.
type ServerSpec struct {
	// PortPattern is a regular expression with the named group "port" (and "host", optional) that is matched against each line
	// the program prints on its standard output or standard error; empty means DefaultPortPattern.
	PortPattern string
	// Timeout is how long to wait for the line; zero means DefaultServerTimeout.
	Timeout time.Duration
}

// ServerProcess is the connection to a server program that StartServer started: the stream of a Client, and the program.
// Closing it closes the connection and ends the program.
type ServerProcess struct {
	conn net.Conn
	proc *Process
	addr string
	once sync.Once
}

// Read reads from the connection.
func (s *ServerProcess) Read(b []byte) (int, error) { return s.conn.Read(b) }

// Write writes to the connection.
func (s *ServerProcess) Write(b []byte) (int, error) { return s.conn.Write(b) }

// Close closes the connection and ends the program (see Process.Close).
func (s *ServerProcess) Close() error {
	var err error
	s.once.Do(func() { err = s.conn.Close() })
	if perr := s.proc.Close(); err == nil {
		err = perr
	}
	return err
}

// Addr is the address the program listens on.
func (s *ServerProcess) Addr() string { return s.addr }

// PID is the process id of the program.
func (s *ServerProcess) PID() int { return s.proc.PID() }

// Done is closed when the program has ended.
func (s *ServerProcess) Done() <-chan struct{} { return s.proc.Done() }

// ExitCode is the exit status of the program; only meaningful after Done.
func (s *ServerProcess) ExitCode() int { return s.proc.ExitCode() }

// Stop ends the program and waits for it (see Process.Stop).
func (s *ServerProcess) Stop(grace time.Duration) {
	s.proc.Stop(grace)
}

// StartServer starts a program that listens on a port and prints it, reads the port from the lines it prints, and connects to
// it ("executable server" of SPEC §10.31). As for StartProcess, ctx is the life of the program. It fails with the tags
// DapAdapterNotFound and DapAdapterStartFailed (from the start), DapPortPatternInvalid, DapAdapterExited (the program ended
// before it printed a port; the metadata hold its exit status), DapTimeout (it printed none within the timeout) and
// DapAdapterConnectFailed.
func StartServer(ctx context.Context, spec ProcessSpec, server ServerSpec) (*ServerProcess, error) {
	pattern := server.PortPattern
	if pattern == "" {
		pattern = DefaultPortPattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, errors.NewErr("DapPortPatternInvalid", err).SetMetadata("pattern", pattern)
	}
	portIdx, hostIdx := re.SubexpIndex("port"), re.SubexpIndex("host")
	if portIdx < 0 {
		return nil, errors.New("DapPortPatternInvalid", "the pattern has no group named port", "pattern", pattern)
	}
	timeout := server.Timeout
	if timeout <= 0 {
		timeout = DefaultServerTimeout
	}

	type found struct{ host, port string }
	foundCh := make(chan found, 1)
	watch := func(line string) {
		if m := re.FindStringSubmatch(line); m != nil {
			f := found{port: m[portIdx]}
			if hostIdx >= 0 {
				f.host = m[hostIdx]
			}
			select {
			case foundCh <- f:
			default: // the first line that matches is the one
			}
		}
	}
	onOut, onErr := spec.Stdout, spec.Stderr
	spec.Stdout = func(line string) {
		watch(line)
		if onOut != nil {
			onOut(line)
		}
	}
	spec.Stderr = func(line string) {
		watch(line)
		if onErr != nil {
			onErr(line)
		}
	}
	proc, err := StartProcess(ctx, spec)
	if err != nil {
		return nil, err
	}
	var f found
	select {
	case f = <-foundCh:
	case <-proc.Done():
		select { // a line printed just before the program ended still counts
		case f = <-foundCh:
		default:
			if ctx.Err() != nil { // the context ended the program
				return nil, errors.NewErr("DapAdapterStartFailed", ctx.Err()).SetMetadata("program", spec.Program)
			}
			return nil, errors.New("DapAdapterExited", "the server program ended before it printed its port",
				"program", spec.Program, "exitCode", proc.ExitCode())
		}
	case <-time.After(timeout):
		proc.Stop(0)
		return nil, errors.New("DapTimeout", "the server program did not print its port in time", "program", spec.Program, "timeout", timeout.String())
	case <-ctx.Done():
		proc.Stop(0)
		return nil, errors.NewErr("DapAdapterStartFailed", ctx.Err()).SetMetadata("program", spec.Program)
	}
	port, perr := strconv.Atoi(f.port)
	if perr != nil || port < 1 || port > 65535 {
		proc.Stop(0)
		return nil, errors.New("DapAdapterConnectFailed", "the server program printed a port that is not one", "port", f.port, "program", spec.Program)
	}
	host := f.host
	switch host {
	case "", "0.0.0.0", "::", "[::]", "*":
		host = loopback
	}
	conn, err := DialTCP(ctx, host, port)
	if err != nil {
		proc.Stop(0)
		return nil, err
	}
	return &ServerProcess{conn: conn, proc: proc, addr: net.JoinHostPort(host, strconv.Itoa(port))}, nil
}

// AdapterKind says how an adapter is reached.
type AdapterKind int

const (
	// AdapterExecutable is a program that speaks DAP on its standard input and output.
	AdapterExecutable AdapterKind = iota + 1
	// AdapterServer is a server on a TCP port that is already listening.
	AdapterServer
	// AdapterPipe is a server on a Unix socket that is already listening.
	AdapterPipe
	// AdapterExecutableServer is a program that listens on a port and prints it (StartServer).
	AdapterExecutableServer
)

// Adapter says where a debug adapter is and how to reach it; it is what a debugger's descriptor (SPEC §10.31 "The extension
// contract") becomes.
type Adapter struct {
	// Kind picks the members that are used.
	Kind AdapterKind
	// Process is the program of AdapterExecutable and AdapterExecutableServer.
	Process ProcessSpec
	// Server says how to find the port of AdapterExecutableServer.
	Server ServerSpec
	// Host and Port are the address of AdapterServer; an empty Host is 127.0.0.1.
	Host string
	Port int
	// Path is the Unix socket of AdapterPipe.
	Path string
}

// Connect reaches the adapter and starts a Client over the connection. For a program, ctx is its life (see StartProcess).
// It fails with the tags of StartProcess, StartServer, DialTCP and DialUnix, and DapAdapterInvalid for a Kind that is none of
// the four.
func Connect(ctx context.Context, a Adapter, opts ClientOptions) (*Client, error) {
	switch a.Kind {
	case AdapterExecutable:
		p, err := StartProcess(ctx, a.Process)
		if err != nil {
			return nil, err
		}
		return NewClient(p, opts), nil
	case AdapterServer:
		conn, err := DialTCP(ctx, a.Host, a.Port)
		if err != nil {
			return nil, err
		}
		return NewClient(conn, opts), nil
	case AdapterPipe:
		conn, err := DialUnix(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		return NewClient(conn, opts), nil
	case AdapterExecutableServer:
		s, err := StartServer(ctx, a.Process, a.Server)
		if err != nil {
			return nil, err
		}
		return NewClient(s, opts), nil
	}
	return nil, errors.New("DapAdapterInvalid", "the adapter has no known kind", "kind", int(a.Kind))
}
