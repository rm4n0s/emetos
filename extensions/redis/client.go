package redis

import (
	"context"
	"crypto/tls"
	"time"
)

// The modes a connection can have (the "mode" field of the form).
const (
	// ModeStandalone is one server.
	ModeStandalone = "standalone"
	// ModeSentinel asks the Sentinels for the master of a name.
	ModeSentinel = "sentinel"
	// ModeCluster is a Redis Cluster; its keys are scanned master by master.
	ModeCluster = "cluster"
)

// The types a key can have, as TYPE names them.
const (
	TypeString = "string"
	TypeHash   = "hash"
	TypeList   = "list"
	TypeSet    = "set"
	TypeZSet   = "zset"
	TypeStream = "stream"
	// TypeJSON is the type the ReJSON module gives its values.
	TypeJSON = "ReJSON-RL"
	// TypeNone is what TYPE says of a key that does not exist.
	TypeNone = "none"
)

// Spec says how to reach a server: everything the client needs, with the password found by the caller. It is made from a
// connection by BuildSpec.
type Spec struct {
	// Mode is ModeStandalone, ModeSentinel or ModeCluster.
	Mode string
	// Addrs are the addresses ("host:port"): the server, the Sentinels or the nodes to start from.
	Addrs []string
	// MasterName is the master a Sentinel mode asks for.
	MasterName string
	// Username and Password are the ACL user and its password; the password is empty when there is none.
	Username, Password string
	// DB is the database a standalone or Sentinel client selects; a cluster has only database 0.
	DB int
	// TLS is nil for a plain connection.
	TLS *tls.Config
	// ReadOnly makes every database of the server refuse changes.
	ReadOnly bool
	// DialTimeout bounds the opening of a connection; 0 is five seconds.
	DialTimeout time.Duration
}

// Dialer opens a server. The real one is go-redis; the tests use an in-memory server.
type Dialer interface {
	// Dial connects, checks the server answers, and returns the server. The server stays open until Close.
	Dial(ctx context.Context, spec Spec) (Server, error)
}

// DBInfo is a database in the list of an open server.
type DBInfo struct {
	// Index is the number of the database.
	Index int
	// Keys is its number of keys, and Expires the number of those with a TTL.
	Keys, Expires int64
}

// Server is an open connection to a Redis, Valkey or compatible server.
type Server interface {
	// Mode is the mode the server was opened in.
	Mode() string
	// Databases lists the databases that have keys (INFO keyspace); a cluster has database 0 with the keys of all its masters.
	Databases(ctx context.Context) ([]DBInfo, error)
	// DB returns the handle of a database; it opens nothing until it is used. A cluster has only database 0.
	DB(index int) (DB, error)
	// PubSub opens a connection for Pub/Sub; nothing is subscribed until Subscribe or PSubscribe. It is a connection of its own,
	// not one of the pool, and stays open until Close.
	PubSub(ctx context.Context) (Subscription, error)
	// Monitor opens a connection of its own and sends MONITOR on it. A cluster has no monitor (it would see one node): it fails
	// with RedisMonitorUnsupported.
	Monitor(ctx context.Context) (Monitor, error)
	// Close closes every connection.
	Close() error
}

// PubSubMessage is a message a Pub/Sub connection received.
type PubSubMessage struct {
	// Time is when the message arrived.
	Time time.Time
	// Pattern is the pattern that matched, empty for a message of a channel that was subscribed by name.
	Pattern string
	// Channel and Payload are the channel and the text of the message.
	Channel, Payload string
}

// Subscription is a Pub/Sub connection: the channels and patterns it listens to, and the messages that come.
type Subscription interface {
	// Messages delivers the messages in order. The channel is closed when the subscription ends (Close, or a connection that
	// fails: Err says which).
	Messages() <-chan PubSubMessage
	// Subscribe and PSubscribe listen to channels by name and by pattern; Unsubscribe and PUnsubscribe stop.
	Subscribe(ctx context.Context, channels ...string) error
	PSubscribe(ctx context.Context, patterns ...string) error
	Unsubscribe(ctx context.Context, channels ...string) error
	PUnsubscribe(ctx context.Context, patterns ...string) error
	// Err is why Messages was closed; nil after Close.
	Err() error
	// Close ends the subscription and its connection. A connection that does not close well has nothing for the caller to do: the
	// implementation logs it.
	Close()
}

// Monitor is a connection that shows every command the server runs (MONITOR).
type Monitor interface {
	// Lines delivers the lines the server sent, as they are: `1339518083.107412 [0 127.0.0.1:60866] "keys" "*"`. The channel is
	// closed when the monitor ends.
	Lines() <-chan string
	// Err is why Lines was closed; nil after Close.
	Err() error
	// Close ends MONITOR and its connection (see Subscription.Close for its errors).
	Close()
}

// Cursor is where a scan of the keys stands. The zero Cursor is the start. In a cluster a scan goes through the masters one after
// the other: Node is the master it is at.
type Cursor struct {
	// Node is the index of the master a cluster scan is at, 0 for a server that is not a cluster.
	Node int
	// Pos is the cursor SCAN gave for that node.
	Pos uint64
	// End says that the scan is over.
	End bool
}

// Done tells whether the scan is over.
func (c Cursor) Done() bool { return c.End }

// KeyMeta is what the explorer shows of a key.
type KeyMeta struct {
	Type string
	// TTLMillis is the time to live in milliseconds, -1 for a key that has none and -2 for a key that does not exist.
	TTLMillis int64
}

// KeyInfo is what the header of the key editor shows.
type KeyInfo struct {
	Type      string
	TTLMillis int64
	// Memory is MEMORY USAGE in bytes, -1 when the server cannot say; Encoding is OBJECT ENCODING, empty when unknown.
	Memory   int64
	Encoding string
	// Size is the length of the value: bytes of a string, fields of a hash, items of a list, members of a set or sorted set, entries
	// of a stream; -1 when it is not known (a JSON value).
	Size int64
}

// Pair is a field and a value, of a hash or of an entry of a stream.
type Pair struct{ Field, Value string }

// ZMember is a member of a sorted set.
type ZMember struct {
	Member string
	Score  float64
}

// StreamEntry is an entry of a stream.
type StreamEntry struct {
	ID     string
	Fields []Pair
}

// DB is one database of a server: the commands the explorer and the key editor use. Every method that talks to the server takes a
// context that ends when the answer is no longer wanted. The methods that change data fail with ReadOnlyError on a read-only
// connection (see ReadOnly).
type DB interface {
	// Index is the number of the database.
	Index() int
	// DBSize is the number of keys in the database (all the masters, in a cluster).
	DBSize(ctx context.Context) (int64, error)
	// Scan asks the server for one page of keys with SCAN (never KEYS): match is a glob pattern, empty for all keys, and count is
	// the hint COUNT. It returns the keys of that call, which may be none while the scan is not over.
	Scan(ctx context.Context, cur Cursor, match string, count int64) (keys []string, next Cursor, err error)
	// Metas gives the type and the TTL of keys in one round trip.
	Metas(ctx context.Context, keys []string) ([]KeyMeta, error)
	// Info reads the header of a key.
	Info(ctx context.Context, key string) (KeyInfo, error)

	// GetRange reads at most max bytes of a string and tells how long it is.
	GetRange(ctx context.Context, key string, max int64) (data string, size int64, err error)
	// Set writes a string and keeps the TTL the key has (SET KEEPTTL).
	Set(ctx context.Context, key, value string) error

	// HScan reads a page of a hash; a returned cursor of 0 ends the scan.
	HScan(ctx context.Context, key string, cursor uint64, count int64) (fields []Pair, next uint64, err error)
	// HSet sets a field; it tells whether the field is new.
	HSet(ctx context.Context, key, field, value string) (added bool, err error)
	// HDel deletes fields.
	HDel(ctx context.Context, key string, fields ...string) error

	// LRange reads the items from start to stop, both inclusive (a negative index counts from the end).
	LRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	// LPush and RPush add an item at the head, or at the tail.
	LPush(ctx context.Context, key, value string) error
	RPush(ctx context.Context, key, value string) error
	// LSet replaces the item at an index.
	LSet(ctx context.Context, key string, index int64, value string) error
	// LRemoveAt removes the item at an index.
	LRemoveAt(ctx context.Context, key string, index int64) error

	// SScan reads a page of a set; a returned cursor of 0 ends the scan.
	SScan(ctx context.Context, key string, cursor uint64, count int64) (members []string, next uint64, err error)
	// SAdd adds a member; it tells whether it is new. SRem removes members.
	SAdd(ctx context.Context, key, member string) (added bool, err error)
	SRem(ctx context.Context, key string, members ...string) error

	// ZScan reads a page of a sorted set; a returned cursor of 0 ends the scan.
	ZScan(ctx context.Context, key string, cursor uint64, count int64) (members []ZMember, next uint64, err error)
	// ZAdd sets the score of a member; it tells whether the member is new. ZRem removes members.
	ZAdd(ctx context.Context, key, member string, score float64) (added bool, err error)
	ZRem(ctx context.Context, key string, members ...string) error

	// XRevRange reads up to count entries, newest first, with ids up to max ("+" for the newest); an id may start with "(" to
	// leave it out.
	XRevRange(ctx context.Context, key, max string, count int64) ([]StreamEntry, error)
	// XAdd adds an entry with the id ("*" for the server's) and returns the id it has.
	XAdd(ctx context.Context, key, id string, fields []Pair) (string, error)
	// XDel deletes entries.
	XDel(ctx context.Context, key string, ids ...string) error

	// JSONGet reads the whole ReJSON value as JSON text, and JSONSet writes it.
	JSONGet(ctx context.Context, key string) (string, error)
	JSONSet(ctx context.Context, key, doc string) error

	// Exists tells whether a key exists.
	Exists(ctx context.Context, key string) (bool, error)
	// Rename gives the key another name, unless that name is taken (RENAMENX); it tells whether it was renamed.
	Rename(ctx context.Context, key, newKey string) (bool, error)
	// Expire gives the key a time to live; Persist removes it. Persist tells whether the key had one.
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Persist(ctx context.Context, key string) (bool, error)
	// Del deletes keys and tells how many existed.
	Del(ctx context.Context, keys ...string) (int64, error)

	// Do sends one command as the console does and returns the reply with its RESP type. An error reply of the server is a Reply
	// of kind ReplyError and a nil error; the error is for a command that could not be sent or read (a closed connection, a
	// cancelled context, a read-only connection that refuses a command: RedisReadOnly). There is no read timeout of its own: a
	// blocking command (BLPOP key 0) waits until ctx ends.
	Do(ctx context.Context, args []string) (Reply, error)
}
