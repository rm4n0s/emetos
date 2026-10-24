package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"

	"github.com/rm4n0s/errors"
)

const (
	// Version is the version of the protocol. A server answers a message of another version with an error.
	Version = 1
	// MaxMessageSize is the largest message, in bytes, a server or a client accepts (without the line end).
	MaxMessageSize = 1 << 20
)

// The types of messages.
const (
	// TypeOpen is the request of a launch: open what the arguments say.
	TypeOpen = "open"
	// TypeOK is the reply to a request that is carried out and not waited for.
	TypeOK = "ok"
	// TypeDone is the reply to a request that waited (--wait): what it opened is closed.
	TypeDone = "done"
	// TypeError is the reply to a request that failed; Message says why.
	TypeError = "error"
)

// Request is the message of a client to the instance: one JSON object on one line.
type Request struct {
	// Version is the protocol version (Version).
	Version int `json:"version"`
	// Type is TypeOpen.
	Type string `json:"type"`
	// PID is the process id of the client, for the log of the instance.
	PID int `json:"pid"`
	// Cwd is the working directory of the client. The paths in Open are made absolute against it already;
	// it is handed over for the instance's log and for what a later version resolves.
	Cwd string `json:"cwd"`
	// Open says what to open; it is required by TypeOpen.
	Open *OpenArgs `json:"open,omitempty"`
}

// OpenArgs are the arguments of a launch that the instance carries out (SPEC §10.11). Every path is absolute.
type OpenArgs struct {
	// Folder is the folder to open as the workspace, or empty.
	Folder string `json:"folder,omitempty"`
	// Files are the files to open, in order; the first is the one that is shown.
	Files []FileTarget `json:"files,omitempty"`
	// Diff holds the two paths of --diff, or none.
	Diff []string `json:"diff,omitempty"`
	// Merge holds the four paths of --merge (the two sides, the base and the result), or none.
	Merge []string `json:"merge,omitempty"`
	// Wait asks the reply to come only when the files that were opened are closed (--wait).
	Wait bool `json:"wait,omitempty"`
	// NewWindow is --new-window and ReuseWindow is --reuse-window.
	NewWindow   bool `json:"newWindow,omitempty"`
	ReuseWindow bool `json:"reuseWindow,omitempty"`
	// Profile is the name given to --profile, or empty.
	Profile string `json:"profile,omitempty"`
}

// FileTarget is a file to open and the position to show in it.
type FileTarget struct {
	// Path is the absolute path of the file.
	Path string `json:"path"`
	// Line and Column count from 1; zero is no position.
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`
}

// Reply is the message of the instance to a client.
type Reply struct {
	// Version is the protocol version (Version).
	Version int `json:"version"`
	// Type is TypeOK, TypeDone or TypeError.
	Type string `json:"type"`
	// ExitCode is the exit code the client ends with.
	ExitCode int `json:"exitCode"`
	// Message is the text for the user (an error, a notice), or empty.
	Message string `json:"message,omitempty"`
}

// OK is a reply for a request that is carried out.
func OK() Reply { return Reply{Version: Version, Type: TypeOK} }

// Done is a reply for a request that waited and whose files are closed.
func Done(exitCode int) Reply { return Reply{Version: Version, Type: TypeDone, ExitCode: exitCode} }

// Failed is the reply for a request that cannot be carried out; the client ends with exit code 1.
func Failed(message string) Reply {
	return Reply{Version: Version, Type: TypeError, ExitCode: 1, Message: message}
}

// writeMessage writes v as one line of JSON.
func writeMessage(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return errors.NewErr("IPCMessageInvalid", err)
	}
	if len(data) > MaxMessageSize {
		return errors.New("IPCMessageTooLarge", "the message is larger than the limit", "size", len(data), "limit", MaxMessageSize)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return errors.NewErr("IPCWriteFailed", err)
	}
	return nil
}

// readMessage reads one line of at most MaxMessageSize bytes and decodes it into v. A closed connection before
// any byte is io.EOF (wrapped as IPCConnectionClosed); a longer line is IPCMessageTooLarge, a line that is not
// the JSON of v is IPCMessageInvalid.
func readMessage(r *bufio.Reader, v any) error {
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > MaxMessageSize+1 {
			return errors.New("IPCMessageTooLarge", "the message is larger than the limit", "limit", MaxMessageSize)
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(line) == 0 {
			return errors.NewErr("IPCConnectionClosed", err)
		}
		return errors.NewErr("IPCReadFailed", err)
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	if err := json.Unmarshal(line, v); err != nil {
		return errors.NewErr("IPCMessageInvalid", err)
	}
	return nil
}
