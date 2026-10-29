package redis

import (
	"context"
	"time"

	"github.com/rm4n0s/errors"
)

// ReadOnly wraps a server so that none of its databases changes anything: every method that writes fails with RedisReadOnly before
// a command is sent (SPEC §10.23 "Connections": read-only). It is the last guard; the editors also hide their buttons.
func ReadOnly(s Server) Server { return &readOnlyServer{Server: s} }

type readOnlyServer struct{ Server }

// DB implements Server.
func (s *readOnlyServer) DB(index int) (DB, error) {
	db, err := s.Server.DB(index)
	if err != nil {
		return nil, err
	}
	return &readOnlyDB{DB: db}, nil
}

// readOnlyDB is a database that refuses to write. The methods that read come from the database it wraps.
type readOnlyDB struct{ DB }

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
