package ipc

import (
	"bufio"
	"context"
	"net"
	"os"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// Client is a connection to the running instance.
type Client struct {
	conn *net.UnixConn
	br   *bufio.Reader
	opts Options
	once sync.Once
}

func newClient(conn *net.UnixConn, opts Options) *Client {
	return &Client{conn: conn, br: bufio.NewReader(conn), opts: opts}
}

// Dial connects to the instance that listens at loc, without the lock and without ever becoming a server
// (tests and tools; a launch uses Join). The tag is IPCDialFailed.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	if err := validLocation(opts.Location); err != nil {
		return nil, err
	}
	conn, err := dialSocket(ctx, opts.Location.Socket)
	if err != nil {
		return nil, errors.NewErr("IPCDialFailed", err).SetMetadata("path", opts.Location.Socket)
	}
	return newClient(conn, opts), nil
}

// Send sends the request and returns the reply of the instance. A request with no Version, Type or PID gets
// them (Version, TypeOpen, the process id). A request that does not wait (Open.Wait false) gets its reply
// within the I/O timeout; one that waits has no limit but ctx, whose end closes the connection (the instance
// then stops waiting). The reply can be an error reply (TypeError): that is a Reply, not an error here.
// The tags are IPCSendFailed and IPCReplyFailed (the deadline of the connection could not be set, or the
// context ended), those of writeMessage and readMessage (IPCWriteFailed, IPCMessageTooLarge, IPCReadFailed,
// IPCConnectionClosed, IPCMessageInvalid: the connection ended or the reply did not come in time), IPCReplyInvalid and
// IPCVersionUnsupported (the reply is of another version).
func (c *Client) Send(ctx context.Context, req Request) (Reply, error) {
	if req.Version == 0 {
		req.Version = Version
	}
	if req.Type == "" {
		req.Type = TypeOpen
	}
	if req.PID == 0 {
		req.PID = os.Getpid()
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.Close() })
	defer stop()
	timeout := c.opts.ioTimeout()
	if err := c.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return Reply{}, errors.NewErr("IPCSendFailed", err)
	}
	if err := writeMessage(c.conn, req); err != nil {
		return Reply{}, err
	}
	deadline := time.Time{}
	if req.Open == nil || !req.Open.Wait {
		deadline = time.Now().Add(replyTimeout(timeout))
	}
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return Reply{}, errors.NewErr("IPCReplyFailed", err)
	}
	var reply Reply
	if err := readMessage(c.br, &reply); err != nil {
		if ctx.Err() != nil {
			return Reply{}, errors.NewErr("IPCReplyFailed", ctx.Err())
		}
		return Reply{}, err
	}
	if reply.Version != Version {
		return Reply{}, errors.New("IPCVersionUnsupported", "the reply is of another version of the protocol", "version", reply.Version, "supported", Version)
	}
	switch reply.Type {
	case TypeOK, TypeDone, TypeError:
	default:
		return Reply{}, errors.New("IPCReplyInvalid", "the reply has an unknown type", "type", reply.Type)
	}
	return reply, nil
}

// replyTimeout is how long the instance has to answer a request that does not wait: it may have to ask the
// user about the unsaved changes of the folder it replaces, so it is far longer than a message takes.
func replyTimeout(io time.Duration) time.Duration { return 6 * io }

// Close ends the connection. It is safe to call more than once.
func (c *Client) Close() error {
	var err error
	c.once.Do(func() { err = c.conn.Close() })
	return err
}
