package redis

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
	"github.com/rm4n0s/errors"
)

// GoDialer is the Dialer over go-redis (github.com/redis/go-redis/v9): one client per database for a standalone server or a Sentinel
// master, one cluster client for a cluster.
type GoDialer struct{}

var _ Dialer = GoDialer{}

// defaultDialTimeout bounds the opening of a connection.
const defaultDialTimeout = 5 * time.Second

// Dial implements Dialer. It sends PING; a server that refuses (a wrong password, a Sentinel with no master) fails here.
func (GoDialer) Dial(ctx context.Context, spec Spec) (Server, error) {
	s := &goServer{spec: spec, dbs: map[int]*goDB{}}
	db, err := s.DB(spec.DB)
	if err != nil {
		return nil, err
	}
	if err := db.(*goDB).c.Ping(ctx).Err(); err != nil {
		_ = s.Close() // the server never answered; nothing more can be learned from closing it
		return nil, errors.NewErr("RedisPingFailed", err)
	}
	return s, nil
}

// goServer is an open server.
type goServer struct {
	spec Spec

	mu     sync.Mutex
	dbs    map[int]*goDB
	closed bool
}

var _ Server = (*goServer)(nil)

// Mode implements Server.
func (s *goServer) Mode() string { return s.spec.Mode }

// DB implements Server: it makes the client of a database the first time it is asked for.
func (s *goServer) DB(index int) (DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("RedisServerClosed", "the connection is closed")
	}
	if db, ok := s.dbs[index]; ok {
		return db, nil
	}
	if s.spec.Mode == ModeCluster && index != 0 {
		return nil, errors.New("RedisClusterDatabaseUnavailable", "a cluster has only database 0", "db", index)
	}
	db := &goDB{idx: index, spec: s.spec}
	db.c = newClient(s.spec, index, false)
	switch c := db.c.(type) {
	case *goredis.ClusterClient:
		db.nodes = func(ctx context.Context) ([]nodeScanner, error) { return clusterMasters(ctx, c) }
	case *goredis.Client:
		db.nodes = single(c)
	}
	s.dbs[index] = db
	return db, nil
}

// newClient makes the go-redis client of a database for the mode of the spec: a pool of four connections, or for the console (one
// command, thrown away after it) a pool of one with no read timeout, so that a blocking command waits for as long as the user lets it
// and closing the client ends it.
func newClient(spec Spec, index int, console bool) goredis.UniversalClient {
	pool, readTimeout := 4, time.Duration(0)
	if console {
		pool, readTimeout = 1, -1 // -1: no read timeout (go-redis)
	}
	switch spec.Mode {
	case ModeCluster:
		return goredis.NewClusterClient(&goredis.ClusterOptions{
			Addrs: spec.Addrs, Username: spec.Username, Password: spec.Password, TLSConfig: spec.TLS,
			DialTimeout: dialTimeout(spec), ReadTimeout: readTimeout, WriteTimeout: readTimeout, ContextTimeoutEnabled: true, PoolSize: pool,
			DisableIdentity: true, MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
		})
	case ModeSentinel:
		return goredis.NewFailoverClient(&goredis.FailoverOptions{
			MasterName: spec.MasterName, SentinelAddrs: spec.Addrs, Username: spec.Username, Password: spec.Password,
			DB: index, TLSConfig: spec.TLS, DialTimeout: dialTimeout(spec), ReadTimeout: readTimeout, WriteTimeout: readTimeout,
			ContextTimeoutEnabled: true, PoolSize: pool, DisableIdentity: true,
		})
	}
	return goredis.NewClient(&goredis.Options{
		Addr: spec.Addrs[0], Username: spec.Username, Password: spec.Password, DB: index, TLSConfig: spec.TLS,
		DialTimeout: dialTimeout(spec), ReadTimeout: readTimeout, WriteTimeout: readTimeout, ContextTimeoutEnabled: true, PoolSize: pool,
		DisableIdentity: true, MaintNotificationsConfig: &maintnotifications.Config{Mode: maintnotifications.ModeDisabled},
	})
}

func dialTimeout(spec Spec) time.Duration {
	if spec.DialTimeout > 0 {
		return spec.DialTimeout
	}
	return defaultDialTimeout
}

// single is the list of nodes of a server that is not a cluster: itself.
func single(c *goredis.Client) func(context.Context) ([]nodeScanner, error) {
	return func(context.Context) ([]nodeScanner, error) { return []nodeScanner{goNode{c}}, nil }
}

// clusterMasters lists the masters of a cluster in the order of their addresses.
func clusterMasters(ctx context.Context, cc *goredis.ClusterClient) ([]nodeScanner, error) {
	var mu sync.Mutex
	var nodes []goNode
	err := cc.ForEachMaster(ctx, func(_ context.Context, c *goredis.Client) error {
		mu.Lock()
		nodes = append(nodes, goNode{c})
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, errors.NewErr("RedisClusterNodesFailed", err)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].c.Options().Addr < nodes[j].c.Options().Addr })
	out := make([]nodeScanner, len(nodes))
	for i, n := range nodes {
		out[i] = n
	}
	return out, nil
}

// Databases implements Server.
func (s *goServer) Databases(ctx context.Context) ([]DBInfo, error) {
	db, err := s.DB(s.spec.DB)
	if err != nil {
		return nil, err
	}
	g := db.(*goDB)
	if s.spec.Mode == ModeCluster {
		n, err := g.DBSize(ctx)
		if err != nil {
			return nil, err
		}
		return []DBInfo{{Index: 0, Keys: n}}, nil
	}
	text, err := g.c.Info(ctx, "keyspace").Result()
	if err != nil {
		return nil, errors.NewErr("RedisKeyspaceFailed", err)
	}
	return ParseKeyspace(text), nil
}

// Close implements Server.
func (s *goServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	var first error
	for _, db := range s.dbs {
		if err := db.c.Close(); err != nil && first == nil {
			first = errors.NewErr("RedisCloseFailed", err)
		}
	}
	s.dbs = map[int]*goDB{}
	return first
}

// ParseKeyspace reads the text INFO keyspace returns ("db0:keys=3,expires=1,avg_ttl=0") into the databases, in the order of their
// numbers. A line it cannot read is left out.
func ParseKeyspace(text string) []DBInfo {
	var out []DBInfo
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		name, rest, ok := strings.Cut(line, ":")
		if !ok || !strings.HasPrefix(name, "db") {
			continue
		}
		idx, err := strconv.Atoi(name[2:])
		if err != nil {
			continue
		}
		info := DBInfo{Index: idx}
		for _, kv := range strings.Split(rest, ",") {
			k, v, _ := strings.Cut(kv, "=")
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			switch k {
			case "keys":
				info.Keys = n
			case "expires":
				info.Expires = n
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// nodeScanner is one node of a scan: a server, or a master of a cluster.
type nodeScanner interface {
	// scan is SCAN on that node.
	scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
	// dbsize is DBSIZE on that node.
	dbsize(ctx context.Context) (int64, error)
}

// goNode is a nodeScanner over a go-redis client.
type goNode struct{ c *goredis.Client }

func (n goNode) scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error) {
	return n.c.Scan(ctx, cursor, match, count).Result()
}

func (n goNode) dbsize(ctx context.Context) (int64, error) { return n.c.DBSize(ctx).Result() }

// scanNodes does one step of a scan over nodes: SCAN on the node of the cursor, then the cursor of the next call. A scan of a node that
// ends goes on at the start of the next node, and the scan is over when the last node ends. Its errors have no tag: the caller
// wraps them.
func scanNodes(ctx context.Context, nodes []nodeScanner, cur Cursor, match string, count int64) ([]string, Cursor, error) {
	if cur.End || cur.Node < 0 || cur.Node >= len(nodes) {
		return nil, Cursor{End: true}, nil
	}
	keys, pos, err := nodes[cur.Node].scan(ctx, cur.Pos, match, count)
	if err != nil {
		return nil, cur, err
	}
	switch {
	case pos != 0:
		return keys, Cursor{Node: cur.Node, Pos: pos}, nil
	case cur.Node+1 < len(nodes):
		return keys, Cursor{Node: cur.Node + 1}, nil
	}
	return keys, Cursor{Node: cur.Node, End: true}, nil
}

// goDB is one database of a go-redis server.
type goDB struct {
	idx int
	c   goredis.UniversalClient
	// spec is how to reach the server, for the clients of the console's commands.
	spec Spec

	protoMu sync.Mutex
	// resp3 is 0 until the protocol is known, then 1 for RESP2 and 2 for RESP3.
	resp3 int

	mu    sync.Mutex
	nodes func(ctx context.Context) ([]nodeScanner, error)
	// list is the nodes of the scan in progress (a cluster's masters), looked up again when a scan starts.
	list []nodeScanner
}

var _ DB = (*goDB)(nil)

// Index implements DB.
func (d *goDB) Index() int { return d.idx }

// scanNodeList returns the nodes a scan at the cursor goes through; the list is read again when the scan starts.
func (d *goDB) scanNodeList(ctx context.Context, cur Cursor) ([]nodeScanner, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.list == nil || (cur.Node == 0 && cur.Pos == 0) {
		list, err := d.nodes(ctx)
		if err != nil {
			return nil, err
		}
		d.list = list
	}
	return d.list, nil
}

// DBSize implements DB.
func (d *goDB) DBSize(ctx context.Context) (int64, error) {
	nodes, err := d.scanNodeList(ctx, Cursor{})
	if err != nil {
		return 0, err
	}
	var total int64
	for _, n := range nodes {
		size, err := n.dbsize(ctx)
		if err != nil {
			return 0, errors.NewErr("RedisDBSizeFailed", err)
		}
		total += size
	}
	return total, nil
}

// Scan implements DB.
func (d *goDB) Scan(ctx context.Context, cur Cursor, match string, count int64) ([]string, Cursor, error) {
	nodes, err := d.scanNodeList(ctx, cur)
	if err != nil {
		return nil, cur, err
	}
	keys, next, err := scanNodes(ctx, nodes, cur, match, count)
	if err != nil {
		return nil, cur, errors.NewErr("RedisScanFailed", err)
	}
	return keys, next, nil
}

// Metas implements DB.
func (d *goDB) Metas(ctx context.Context, keys []string) ([]KeyMeta, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := d.c.Pipeline()
	types := make([]*goredis.StatusCmd, len(keys))
	ttls := make([]*goredis.DurationCmd, len(keys))
	for i, k := range keys {
		types[i] = pipe.Type(ctx, k)
		ttls[i] = pipe.PTTL(ctx, k)
	}
	if _, err := pipe.Exec(ctx); err != nil && !isNilOnly(err) {
		return nil, errors.NewErr("RedisMetasFailed", err)
	}
	out := make([]KeyMeta, len(keys))
	for i := range keys {
		out[i] = KeyMeta{Type: types[i].Val(), TTLMillis: ttlMillis(ttls[i])}
	}
	return out, nil
}

// isNilOnly tells whether a pipeline's error is only the reply "nil" of a command whose key is gone.
func isNilOnly(err error) bool { return errors.Is(err, goredis.Nil) }

// ttlMillis reads a PTTL reply: go-redis turns the reply -1 and -2 into the durations -1 and -2 (nanoseconds, so not scaled).
func ttlMillis(c *goredis.DurationCmd) int64 {
	d := c.Val()
	if d < 0 {
		return int64(d)
	}
	return d.Milliseconds()
}

// Info implements DB.
func (d *goDB) Info(ctx context.Context, key string) (KeyInfo, error) {
	typ, err := d.c.Type(ctx, key).Result()
	if err != nil {
		return KeyInfo{}, errors.NewErr("RedisInfoFailed", err)
	}
	info := KeyInfo{Type: typ, TTLMillis: -2, Memory: -1, Size: -1}
	if typ == TypeNone {
		return info, nil
	}
	pipe := d.c.Pipeline()
	ttl := pipe.PTTL(ctx, key)
	mem := pipe.MemoryUsage(ctx, key)
	enc := pipe.ObjectEncoding(ctx, key)
	var size *goredis.IntCmd
	switch typ {
	case TypeString:
		size = pipe.StrLen(ctx, key)
	case TypeHash:
		size = pipe.HLen(ctx, key)
	case TypeList:
		size = pipe.LLen(ctx, key)
	case TypeSet:
		size = pipe.SCard(ctx, key)
	case TypeZSet:
		size = pipe.ZCard(ctx, key)
	case TypeStream:
		size = pipe.XLen(ctx, key)
	}
	// MEMORY USAGE and OBJECT ENCODING are not answered by every server (managed services, modules): their errors are not the key's.
	_, _ = pipe.Exec(ctx)
	if err := ttl.Err(); err != nil {
		return KeyInfo{}, errors.NewErr("RedisInfoTTLFailed", err)
	}
	info.TTLMillis = ttlMillis(ttl)
	if mem.Err() == nil {
		info.Memory = mem.Val()
	}
	if enc.Err() == nil {
		info.Encoding = enc.Val()
	}
	if size != nil && size.Err() == nil {
		info.Size = size.Val()
	}
	return info, nil
}

// GetRange implements DB.
func (d *goDB) GetRange(ctx context.Context, key string, max int64) (string, int64, error) {
	pipe := d.c.Pipeline()
	size := pipe.StrLen(ctx, key)
	data := pipe.GetRange(ctx, key, 0, max-1)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", 0, errors.NewErr("RedisGetFailed", err)
	}
	return data.Val(), size.Val(), nil
}

// Set implements DB.
func (d *goDB) Set(ctx context.Context, key, value string) error {
	if err := d.c.SetArgs(ctx, key, value, goredis.SetArgs{KeepTTL: true}).Err(); err != nil {
		return errors.NewErr("RedisSetFailed", err)
	}
	return nil
}

// HScan implements DB.
func (d *goDB) HScan(ctx context.Context, key string, cursor uint64, count int64) ([]Pair, uint64, error) {
	flat, next, err := d.c.HScan(ctx, key, cursor, "", count).Result()
	if err != nil {
		return nil, 0, errors.NewErr("RedisHScanFailed", err)
	}
	out := make([]Pair, 0, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		out = append(out, Pair{Field: flat[i], Value: flat[i+1]})
	}
	return out, next, nil
}

// HSet implements DB.
func (d *goDB) HSet(ctx context.Context, key, field, value string) (bool, error) {
	n, err := d.c.HSet(ctx, key, field, value).Result()
	if err != nil {
		return false, errors.NewErr("RedisHSetFailed", err)
	}
	return n > 0, nil
}

// HDel implements DB.
func (d *goDB) HDel(ctx context.Context, key string, fields ...string) error {
	if err := d.c.HDel(ctx, key, fields...).Err(); err != nil {
		return errors.NewErr("RedisHDelFailed", err)
	}
	return nil
}

// LRange implements DB.
func (d *goDB) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	items, err := d.c.LRange(ctx, key, start, stop).Result()
	if err != nil {
		return nil, errors.NewErr("RedisLRangeFailed", err)
	}
	return items, nil
}

// LPush implements DB.
func (d *goDB) LPush(ctx context.Context, key, value string) error {
	if err := d.c.LPush(ctx, key, value).Err(); err != nil {
		return errors.NewErr("RedisLPushFailed", err)
	}
	return nil
}

// RPush implements DB.
func (d *goDB) RPush(ctx context.Context, key, value string) error {
	if err := d.c.RPush(ctx, key, value).Err(); err != nil {
		return errors.NewErr("RedisRPushFailed", err)
	}
	return nil
}

// LSet implements DB.
func (d *goDB) LSet(ctx context.Context, key string, index int64, value string) error {
	if err := d.c.LSet(ctx, key, index, value).Err(); err != nil {
		return errors.NewErr("RedisLSetFailed", err)
	}
	return nil
}

// LRemoveAt implements DB: it puts a marker that no value has at the index and removes the marker, the way the Redis documentation
// says to delete by index. The marker is unique to the call.
func (d *goDB) LRemoveAt(ctx context.Context, key string, index int64) error {
	marker := "\x00emetos-remove-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := d.c.LSet(ctx, key, index, marker).Err(); err != nil {
		return errors.NewErr("RedisLRemoveMarkFailed", err)
	}
	if err := d.c.LRem(ctx, key, 1, marker).Err(); err != nil {
		return errors.NewErr("RedisLRemoveFailed", err)
	}
	return nil
}

// SScan implements DB.
func (d *goDB) SScan(ctx context.Context, key string, cursor uint64, count int64) ([]string, uint64, error) {
	members, next, err := d.c.SScan(ctx, key, cursor, "", count).Result()
	if err != nil {
		return nil, 0, errors.NewErr("RedisSScanFailed", err)
	}
	return members, next, nil
}

// SAdd implements DB.
func (d *goDB) SAdd(ctx context.Context, key, member string) (bool, error) {
	n, err := d.c.SAdd(ctx, key, member).Result()
	if err != nil {
		return false, errors.NewErr("RedisSAddFailed", err)
	}
	return n > 0, nil
}

// SRem implements DB.
func (d *goDB) SRem(ctx context.Context, key string, members ...string) error {
	args := make([]interface{}, len(members))
	for i, m := range members {
		args[i] = m
	}
	if err := d.c.SRem(ctx, key, args...).Err(); err != nil {
		return errors.NewErr("RedisSRemFailed", err)
	}
	return nil
}

// ZScan implements DB.
func (d *goDB) ZScan(ctx context.Context, key string, cursor uint64, count int64) ([]ZMember, uint64, error) {
	flat, next, err := d.c.ZScan(ctx, key, cursor, "", count).Result()
	if err != nil {
		return nil, 0, errors.NewErr("RedisZScanFailed", err)
	}
	out := make([]ZMember, 0, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		score, err := strconv.ParseFloat(flat[i+1], 64)
		if err != nil {
			return nil, 0, errors.NewErr("RedisZScanScoreInvalid", err)
		}
		out = append(out, ZMember{Member: flat[i], Score: score})
	}
	return out, next, nil
}

// ZAdd implements DB.
func (d *goDB) ZAdd(ctx context.Context, key, member string, score float64) (bool, error) {
	n, err := d.c.ZAdd(ctx, key, goredis.Z{Score: score, Member: member}).Result()
	if err != nil {
		return false, errors.NewErr("RedisZAddFailed", err)
	}
	return n > 0, nil
}

// ZRem implements DB.
func (d *goDB) ZRem(ctx context.Context, key string, members ...string) error {
	args := make([]interface{}, len(members))
	for i, m := range members {
		args[i] = m
	}
	if err := d.c.ZRem(ctx, key, args...).Err(); err != nil {
		return errors.NewErr("RedisZRemFailed", err)
	}
	return nil
}

// XRevRange implements DB. It reads the reply itself because go-redis turns the fields of an entry into a map, which loses their
// order and the fields that repeat.
func (d *goDB) XRevRange(ctx context.Context, key, max string, count int64) ([]StreamEntry, error) {
	reply, err := d.c.Do(ctx, "XREVRANGE", key, max, "-", "COUNT", count).Slice()
	if err != nil {
		return nil, errors.NewErr("RedisXRevRangeFailed", err)
	}
	out, err := parseEntries(reply)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseEntries reads the reply of XRANGE and XREVRANGE: an array of [id, [field, value, ...]].
func parseEntries(reply []interface{}) ([]StreamEntry, error) {
	out := make([]StreamEntry, 0, len(reply))
	for _, item := range reply {
		pair, ok := item.([]interface{})
		if !ok || len(pair) != 2 {
			return nil, errors.New("RedisReplyInvalid", "an entry of a stream is [id, fields]", "type", fmt.Sprintf("%T", item))
		}
		id, ok := pair[0].(string)
		if !ok {
			return nil, errors.New("RedisReplyInvalid", "the id of an entry is a string", "type", fmt.Sprintf("%T", pair[0]))
		}
		e := StreamEntry{ID: id}
		switch fields := pair[1].(type) {
		case []interface{}:
			for i := 0; i+1 < len(fields); i += 2 {
				f, _ := fields[i].(string)
				v, _ := fields[i+1].(string)
				e.Fields = append(e.Fields, Pair{Field: f, Value: v})
			}
		case nil:
		default:
			return nil, errors.New("RedisReplyInvalid", "the fields of an entry are an array", "type", fmt.Sprintf("%T", fields))
		}
		out = append(out, e)
	}
	return out, nil
}

// XAdd implements DB.
func (d *goDB) XAdd(ctx context.Context, key, id string, fields []Pair) (string, error) {
	args := make([]interface{}, 0, 3+2*len(fields))
	args = append(args, "XADD", key, id)
	for _, f := range fields {
		args = append(args, f.Field, f.Value)
	}
	got, err := d.c.Do(ctx, args...).Text()
	if err != nil {
		return "", errors.NewErr("RedisXAddFailed", err)
	}
	return got, nil
}

// XDel implements DB.
func (d *goDB) XDel(ctx context.Context, key string, ids ...string) error {
	if err := d.c.XDel(ctx, key, ids...).Err(); err != nil {
		return errors.NewErr("RedisXDelFailed", err)
	}
	return nil
}

// JSONGet implements DB.
func (d *goDB) JSONGet(ctx context.Context, key string) (string, error) {
	doc, err := d.c.Do(ctx, "JSON.GET", key).Text()
	if err != nil {
		return "", errors.NewErr("RedisJSONGetFailed", err)
	}
	return doc, nil
}

// JSONSet implements DB.
func (d *goDB) JSONSet(ctx context.Context, key, doc string) error {
	if err := d.c.Do(ctx, "JSON.SET", key, "$", doc).Err(); err != nil {
		return errors.NewErr("RedisJSONSetFailed", err)
	}
	return nil
}

// Exists implements DB.
func (d *goDB) Exists(ctx context.Context, key string) (bool, error) {
	n, err := d.c.Exists(ctx, key).Result()
	if err != nil {
		return false, errors.NewErr("RedisExistsFailed", err)
	}
	return n > 0, nil
}

// Rename implements DB.
func (d *goDB) Rename(ctx context.Context, key, newKey string) (bool, error) {
	ok, err := d.c.RenameNX(ctx, key, newKey).Result()
	if err != nil {
		return false, errors.NewErr("RedisRenameFailed", err)
	}
	return ok, nil
}

// Expire implements DB.
func (d *goDB) Expire(ctx context.Context, key string, ttl time.Duration) error {
	ok, err := d.c.PExpire(ctx, key, ttl).Result()
	if err != nil {
		return errors.NewErr("RedisExpireFailed", err)
	}
	if !ok {
		return errors.New("RedisKeyMissing", "the key does not exist")
	}
	return nil
}

// Persist implements DB.
func (d *goDB) Persist(ctx context.Context, key string) (bool, error) {
	ok, err := d.c.Persist(ctx, key).Result()
	if err != nil {
		return false, errors.NewErr("RedisPersistFailed", err)
	}
	return ok, nil
}

// Del implements DB.
func (d *goDB) Del(ctx context.Context, keys ...string) (int64, error) {
	n, err := d.c.Del(ctx, keys...).Result()
	if err != nil {
		return 0, errors.NewErr("RedisDelFailed", err)
	}
	return n, nil
}

// ---- the console's commands ----

// Do implements DB. The command runs on a client of its own, made for it and closed after it, with no read timeout: when ctx ends the
// client is closed, which is the only way to stop a read that waits for the server (go-redis stops a read only at a deadline, not when
// a context is cancelled).
func (d *goDB) Do(ctx context.Context, args []string) (Reply, error) {
	if len(args) == 0 {
		return Reply{}, errors.New("RedisCommandEmpty", "there is no command to send")
	}
	iargs := make([]interface{}, len(args))
	for i, a := range args {
		iargs[i] = a
	}
	client := newClient(d.spec, d.idx, true)
	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close() // closing is what ends the read; its error says nothing more
		case <-finished:
		}
	}()
	cmd := client.Do(ctx, iargs...)
	close(finished)
	_ = client.Close() // the client is thrown away after one command; a second close after a cancel is refused, and that is fine
	v, err := cmd.Result()
	switch {
	case err == nil:
		resp3 := false
		if setCommands[strings.ToUpper(args[0])] {
			resp3 = d.isRESP3(ctx)
		}
		return ConvertReply(args, v, resp3), nil
	case errors.Is(err, goredis.Nil):
		return Reply{Kind: ReplyNil}, nil
	}
	var serverErr goredis.Error
	if errors.As(err, &serverErr) {
		return Reply{Kind: ReplyError, Str: err.Error()}, nil
	}
	return Reply{}, errors.NewErr("RedisDoFailed", err).SetMetadata("command", strings.ToUpper(args[0]))
}

// isRESP3 tells whether the connections of the database speak RESP3: HELLO with no argument answers a map in RESP3 and a flat array in
// RESP2 (and an error before Redis 6). The answer is kept.
func (d *goDB) isRESP3(ctx context.Context) bool {
	d.protoMu.Lock()
	defer d.protoMu.Unlock()
	if d.resp3 == 0 {
		d.resp3 = 1
		if v, err := d.c.Do(ctx, "HELLO").Result(); err == nil {
			if _, ok := v.(map[interface{}]interface{}); ok {
				d.resp3 = 2
			}
		}
	}
	return d.resp3 == 2
}

// ---- Pub/Sub ----

// PubSub implements Server.
func (s *goServer) PubSub(ctx context.Context) (Subscription, error) {
	db, err := s.DB(s.spec.DB)
	if err != nil {
		return nil, err
	}
	ps := db.(*goDB).c.Subscribe(ctx)
	sub := &goSub{ps: ps, out: make(chan PubSubMessage, 256)}
	var loopCtx context.Context
	loopCtx, sub.cancel = context.WithCancel(context.Background())
	go sub.run(loopCtx)
	return sub, nil
}

// goSub is a Subscription over a go-redis PubSub.
type goSub struct {
	ps     *goredis.PubSub
	out    chan PubSubMessage
	cancel context.CancelFunc

	mu     sync.Mutex
	err    error
	closed bool
}

var _ Subscription = (*goSub)(nil)

// run reads the messages until the connection fails or the subscription is closed.
func (s *goSub) run(ctx context.Context) {
	defer close(s.out)
	for {
		msg, err := s.ps.ReceiveMessage(ctx)
		if err != nil {
			s.mu.Lock()
			if !s.closed {
				s.err = errors.NewErr("RedisPubSubFailed", err)
			}
			s.mu.Unlock()
			return
		}
		select {
		case s.out <- PubSubMessage{Time: time.Now(), Pattern: msg.Pattern, Channel: msg.Channel, Payload: msg.Payload}:
		case <-ctx.Done():
			return
		}
	}
}

// Messages implements Subscription.
func (s *goSub) Messages() <-chan PubSubMessage { return s.out }

// Subscribe implements Subscription.
func (s *goSub) Subscribe(ctx context.Context, channels ...string) error {
	if err := s.ps.Subscribe(ctx, channels...); err != nil {
		return errors.NewErr("RedisSubscribeFailed", err)
	}
	return nil
}

// PSubscribe implements Subscription.
func (s *goSub) PSubscribe(ctx context.Context, patterns ...string) error {
	if err := s.ps.PSubscribe(ctx, patterns...); err != nil {
		return errors.NewErr("RedisPSubscribeFailed", err)
	}
	return nil
}

// Unsubscribe implements Subscription.
func (s *goSub) Unsubscribe(ctx context.Context, channels ...string) error {
	if err := s.ps.Unsubscribe(ctx, channels...); err != nil {
		return errors.NewErr("RedisUnsubscribeFailed", err)
	}
	return nil
}

// PUnsubscribe implements Subscription.
func (s *goSub) PUnsubscribe(ctx context.Context, patterns ...string) error {
	if err := s.ps.PUnsubscribe(ctx, patterns...); err != nil {
		return errors.NewErr("RedisPUnsubscribeFailed", err)
	}
	return nil
}

// Err implements Subscription.
func (s *goSub) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close implements Subscription.
func (s *goSub) Close() {
	s.mu.Lock()
	already := s.closed
	s.closed = true
	s.mu.Unlock()
	if already {
		return
	}
	s.cancel()
	if err := s.ps.Close(); err != nil {
		slog.Warn("a Pub/Sub connection did not close", "err", err)
	}
}

// ---- Monitor ----

// Monitor implements Server. go-redis's MonitorCmd cannot be stopped while the server is quiet, so MONITOR runs on a connection of
// its own that this file opens and reads: the server's address, the TLS settings, the user and the password are the Spec's. A Sentinel
// or a cluster has no monitor.
func (s *goServer) Monitor(ctx context.Context) (Monitor, error) {
	if s.spec.Mode != ModeStandalone {
		return nil, errors.New("RedisMonitorUnsupported", "MONITOR is available on a standalone server only: a Sentinel or a cluster has more than one node", "mode", s.spec.Mode)
	}
	dialer := &net.Dialer{Timeout: dialTimeout(s.spec)}
	var conn net.Conn
	var err error
	if s.spec.TLS != nil {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: s.spec.TLS.Clone()}).DialContext(ctx, "tcp", s.spec.Addrs[0])
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.spec.Addrs[0])
	}
	if err != nil {
		return nil, errors.NewErr("RedisMonitorDialFailed", err)
	}
	m := &goMonitor{conn: conn, out: make(chan string, 256)}
	r := bufio.NewReader(conn)
	if s.spec.Password != "" {
		auth := []string{"AUTH", s.spec.Password}
		if s.spec.Username != "" {
			auth = []string{"AUTH", s.spec.Username, s.spec.Password}
		}
		if err := roundTrip(ctx, conn, r, auth); err != nil {
			_ = conn.Close() // the connection is of no use; the error above is the one to report
			return nil, err
		}
	}
	if err := roundTrip(ctx, conn, r, []string{"MONITOR"}); err != nil {
		_ = conn.Close() // the connection is of no use; the error above is the one to report
		return nil, err
	}
	go m.run(r)
	return m, nil
}

// roundTrip sends a command and reads its one-line answer; a reply that is not +OK fails. The error holds neither the password nor
// the line the server sent back, which may repeat it.
func roundTrip(ctx context.Context, conn net.Conn, r *bufio.Reader, args []string) error {
	if dl, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(dl); err != nil {
			return errors.NewErr("RedisMonitorFailed", err)
		}
	} else if err := conn.SetDeadline(time.Now().Add(defaultDialTimeout)); err != nil {
		return errors.NewErr("RedisMonitorFailed", err)
	}
	var b strings.Builder
	b.WriteString("*" + strconv.Itoa(len(args)) + "\r\n")
	for _, a := range args {
		b.WriteString("$" + strconv.Itoa(len(a)) + "\r\n" + a + "\r\n")
	}
	if _, err := io.WriteString(conn, b.String()); err != nil {
		return errors.NewErr("RedisMonitorFailed", err)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return errors.NewErr("RedisMonitorFailed", err)
	}
	if !strings.HasPrefix(line, "+OK") {
		if strings.HasPrefix(line, "-") {
			return errors.New("RedisMonitorRefused", "the server refused the command", "command", args[0], "reply", firstWord(strings.TrimPrefix(line, "-")))
		}
		return errors.New("RedisMonitorFailed", "the server sent something other than OK", "command", args[0])
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return errors.NewErr("RedisMonitorFailed", err)
	}
	return nil
}

// firstWord is the kind of an error reply (ERR, NOPERM, WRONGPASS), which names the failure without the text that may repeat an
// argument.
func firstWord(s string) string {
	w, _, _ := strings.Cut(strings.TrimSpace(s), " ")
	return w
}

// goMonitor is a Monitor over a connection of its own.
type goMonitor struct {
	conn net.Conn
	out  chan string

	mu     sync.Mutex
	err    error
	closed bool
}

var _ Monitor = (*goMonitor)(nil)

// run reads the lines the server sends: simple strings, one for each command it runs.
func (m *goMonitor) run(r *bufio.Reader) {
	defer close(m.out)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			m.mu.Lock()
			if !m.closed {
				m.err = errors.NewErr("RedisMonitorFailed", err)
			}
			m.mu.Unlock()
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			line = line[1:]
		}
		m.out <- line
	}
}

// Lines implements Monitor.
func (m *goMonitor) Lines() <-chan string { return m.out }

// Err implements Monitor.
func (m *goMonitor) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// Close implements Monitor. A reader that waits for a full channel is let go: it drains what is left.
func (m *goMonitor) Close() {
	m.mu.Lock()
	already := m.closed
	m.closed = true
	m.mu.Unlock()
	if already {
		return
	}
	if err := m.conn.Close(); err != nil {
		slog.Warn("a monitor connection did not close", "err", err)
	}
	go func() {
		for range m.out {
		}
	}()
}
