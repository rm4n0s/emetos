package redis

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// ReadOnly wraps a server so that none of its databases changes anything: every method that writes fails with RedisReadOnly before
// a command is sent (SPEC §10.23 "Connections": read-only). It is the last guard; the editors also hide their buttons.
func ReadOnly(s Server) Server {
	return &readOnlyServer{Server: s, guard: &commandGuard{flags: map[string]flagsEntry{}}}
}

type readOnlyServer struct {
	Server
	guard *commandGuard
}

// DB implements Server.
func (s *readOnlyServer) DB(index int) (DB, error) {
	db, err := s.Server.DB(index)
	if err != nil {
		return nil, err
	}
	return &readOnlyDB{DB: db, guard: s.guard}, nil
}

// readOnlyDB is a database that refuses to write. The methods that read come from the database it wraps.
type readOnlyDB struct {
	DB
	guard *commandGuard
}

// Do implements DB: the console's commands pass the guard, which refuses the commands the server flags as "write" (COMMAND INFO), and
// those of the built-in lists (RefusedByBuiltinList), before anything is sent. PUBLISH is refused too: it changes what other clients
// receive.
func (d *readOnlyDB) Do(ctx context.Context, args []string) (Reply, error) {
	if len(args) == 0 {
		return d.DB.Do(ctx, args)
	}
	refused, err := d.guard.refuses(ctx, d.DB, args)
	if err != nil {
		return Reply{}, err
	}
	if refused {
		name, sub := commandWords(args)
		if sub != "" && commandHasSubs(name) {
			name += " " + sub
		}
		err := errors.New("RedisReadOnly", readOnlyMessage, "command", name)
		return Reply{}, err
	}
	return d.DB.Do(ctx, args)
}

// flagsEntry is what the guard learned of a command from the server.
type flagsEntry struct {
	// known says that the server described the command; write says that it flagged it as a write.
	known, write bool
}

// commandGuard decides whether a read-only connection may send a command. It asks the server once for each command (COMMAND INFO) and
// keeps the answer for the life of the connection.
type commandGuard struct {
	mu    sync.Mutex
	flags map[string]flagsEntry
}

// commandHasSubs tells whether the command is one that has subcommands, which COMMAND INFO describes as name|sub.
func commandHasSubs(name string) bool {
	switch name {
	case "CONFIG", "CLIENT", "SCRIPT", "FUNCTION", "ACL", "MODULE", "XGROUP", "XINFO", "OBJECT", "MEMORY", "SLOWLOG", "LATENCY", "CLUSTER", "COMMAND", "PUBSUB", "DEBUG":
		return true
	}
	return false
}

// refuses tells whether the command is refused on a read-only connection. The server's flags decide first (a command it flags as
// "write"); a command it does not describe, or a server that cannot say (COMMAND INFO fails), falls back on the built-in lists, which
// also add the commands that are not writes to the server but that change the server (EVAL, CONFIG SET, PUBLISH).
func (g *commandGuard) refuses(ctx context.Context, db DB, args []string) (bool, error) {
	if RefusedByBuiltinList(args) {
		return true, nil
	}
	name, sub := commandWords(args)
	full := strings.ToLower(name)
	if sub != "" && commandHasSubs(name) {
		full += "|" + strings.ToLower(sub)
	}
	g.mu.Lock()
	e, ok := g.flags[full]
	g.mu.Unlock()
	if !ok {
		rep, err := db.Do(ctx, []string{"COMMAND", "INFO", full})
		if err != nil {
			// a connection that fails is the caller's error: the command is not sent
			return false, err
		}
		e = flagsOf(rep)
		g.mu.Lock()
		g.flags[full] = e
		g.mu.Unlock()
	}
	return e.known && e.write, nil
}

// flagsOf reads the reply of COMMAND INFO of one command: an array holding nil for a command the server does not know, else
// [name, arity, [flags...], ...].
func flagsOf(r Reply) flagsEntry {
	if r.Kind != ReplyArray || len(r.Items) == 0 {
		return flagsEntry{}
	}
	info := r.Items[0]
	if info.Kind != ReplyArray || len(info.Items) < 3 {
		return flagsEntry{}
	}
	flags := info.Items[2]
	e := flagsEntry{known: true}
	for _, f := range flags.Items {
		if strings.EqualFold(f.Str, "write") {
			e.write = true
		}
	}
	return e
}

const readOnlyMessage = "the connection is read-only"

// Set implements DB.
func (d *readOnlyDB) Set(_ context.Context, key, value string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "Set")
	return err
}

// HSet implements DB.
func (d *readOnlyDB) HSet(_ context.Context, key, field, value string) (bool, error) {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "HSet")
	return false, err
}

// HDel implements DB.
func (d *readOnlyDB) HDel(_ context.Context, key string, fields ...string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "HDel")
	return err
}

// LPush implements DB.
func (d *readOnlyDB) LPush(_ context.Context, key, value string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "LPush")
	return err
}

// RPush implements DB.
func (d *readOnlyDB) RPush(_ context.Context, key, value string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "RPush")
	return err
}

// LSet implements DB.
func (d *readOnlyDB) LSet(_ context.Context, key string, index int64, value string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "LSet")
	return err
}

// LRemoveAt implements DB.
func (d *readOnlyDB) LRemoveAt(_ context.Context, key string, index int64) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "LRemoveAt")
	return err
}

// SAdd implements DB.
func (d *readOnlyDB) SAdd(_ context.Context, key, member string) (bool, error) {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "SAdd")
	return false, err
}

// SRem implements DB.
func (d *readOnlyDB) SRem(_ context.Context, key string, members ...string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "SRem")
	return err
}

// ZAdd implements DB.
func (d *readOnlyDB) ZAdd(_ context.Context, key, member string, score float64) (bool, error) {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "ZAdd")
	return false, err
}

// ZRem implements DB.
func (d *readOnlyDB) ZRem(_ context.Context, key string, members ...string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "ZRem")
	return err
}

// XAdd implements DB.
func (d *readOnlyDB) XAdd(_ context.Context, key, id string, fields []Pair) (string, error) {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "XAdd")
	return "", err
}

// XDel implements DB.
func (d *readOnlyDB) XDel(_ context.Context, key string, ids ...string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "XDel")
	return err
}

// JSONSet implements DB.
func (d *readOnlyDB) JSONSet(_ context.Context, key, doc string) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "JSONSet")
	return err
}

// Rename implements DB.
func (d *readOnlyDB) Rename(_ context.Context, key, newKey string) (bool, error) {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "Rename")
	return false, err
}

// Expire implements DB.
func (d *readOnlyDB) Expire(_ context.Context, key string, ttl time.Duration) error {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "Expire")
	return err
}

// Persist implements DB.
func (d *readOnlyDB) Persist(_ context.Context, key string) (bool, error) {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "Persist")
	return false, err
}

// Del implements DB.
func (d *readOnlyDB) Del(_ context.Context, keys ...string) (int64, error) {
	err := errors.New("RedisReadOnly", readOnlyMessage, "command", "Del")
	return 0, err
}
