package redis

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// CommandDoc is a command the server knows, for completion: its name in capitals and, when the server has COMMAND DOCS, its summary.
type CommandDoc struct {
	Name    string
	Summary string
}

// ParseCommandDocs reads the reply of COMMAND DOCS: a RESP3 map from the name of a command to its documentation (a map), or in RESP2
// a flat array of the name and the documentation (a flat array of keys and values). It keeps the name and the summary. The commands
// come sorted by name; an entry it cannot read is left out.
func ParseCommandDocs(r Reply) []CommandDoc {
	var out []CommandDoc
	add := func(name, doc Reply) {
		if name.Kind != ReplyString && name.Kind != ReplyStatus {
			return
		}
		d := CommandDoc{Name: strings.ToUpper(name.Str)}
		for _, kv := range keyValues(doc) {
			if strings.EqualFold(kv[0], "summary") {
				d.Summary = kv[1]
			}
		}
		out = append(out, d)
	}
	switch r.Kind {
	case ReplyMap:
		for _, p := range r.Pairs {
			add(p.Key, p.Value)
		}
	case ReplyArray:
		for i := 0; i+1 < len(r.Items); i += 2 {
			add(r.Items[i], r.Items[i+1])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// keyValues reads a map reply, or a flat array of keys and values, as pairs of text; a value that is not text is skipped.
func keyValues(r Reply) [][2]string {
	var out [][2]string
	switch r.Kind {
	case ReplyMap:
		for _, p := range r.Pairs {
			if p.Value.Kind == ReplyString || p.Value.Kind == ReplyStatus {
				out = append(out, [2]string{p.Key.Str, p.Value.Str})
			}
		}
	case ReplyArray:
		for i := 0; i+1 < len(r.Items); i += 2 {
			k, v := r.Items[i], r.Items[i+1]
			if (k.Kind == ReplyString || k.Kind == ReplyStatus) && (v.Kind == ReplyString || v.Kind == ReplyStatus) {
				out = append(out, [2]string{k.Str, v.Str})
			}
		}
	}
	return out
}

// ParseCommandList reads the reply of COMMAND with no argument: an array whose elements start with the name of a command.
func ParseCommandList(r Reply) []CommandDoc {
	var out []CommandDoc
	if r.Kind == ReplyArray {
		for _, it := range r.Items {
			if it.Kind == ReplyArray && len(it.Items) > 0 && (it.Items[0].Kind == ReplyString || it.Items[0].Kind == ReplyStatus) {
				if name := it.Items[0].Str; !strings.Contains(name, "|") {
					out = append(out, CommandDoc{Name: strings.ToUpper(name)})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// builtinCommandNames are the commands the console completes when the server cannot list its own (COMMAND DOCS and COMMAND fail, or
// the connection has not answered yet).
var builtinCommandNames = strings.Fields(`
	APPEND AUTH BGREWRITEAOF BGSAVE BITCOUNT BITFIELD BITOP BITPOS BLMOVE BLMPOP BLPOP BRPOP BRPOPLPUSH BZMPOP BZPOPMAX BZPOPMIN
	CLIENT CLUSTER COMMAND CONFIG COPY DBSIZE DEBUG DECR DECRBY DEL DISCARD DUMP ECHO EVAL EVALSHA EXEC EXISTS EXPIRE EXPIREAT EXPIRETIME
	FCALL FLUSHALL FLUSHDB FUNCTION GEOADD GEODIST GEOHASH GEOPOS GEORADIUS GEOSEARCH GET GETBIT GETDEL GETEX GETRANGE GETSET
	HDEL HELLO HEXISTS HGET HGETALL HINCRBY HINCRBYFLOAT HKEYS HLEN HMGET HMSET HRANDFIELD HSCAN HSET HSETNX HSTRLEN HVALS
	INCR INCRBY INCRBYFLOAT INFO KEYS LASTSAVE LATENCY LCS LINDEX LINSERT LLEN LMOVE LMPOP LPOP LPOS LPUSH LPUSHX LRANGE LREM LSET LTRIM
	MEMORY MGET MIGRATE MODULE MONITOR MOVE MSET MSETNX MULTI OBJECT PERSIST PEXPIRE PEXPIREAT PEXPIRETIME PFADD PFCOUNT PFMERGE PING
	PSETEX PSUBSCRIBE PTTL PUBLISH PUBSUB PUNSUBSCRIBE QUIT RANDOMKEY RENAME RENAMENX RESET RESTORE RPOP RPOPLPUSH RPUSH RPUSHX
	SADD SAVE SCAN SCARD SCRIPT SDIFF SDIFFSTORE SELECT SET SETBIT SETEX SETNX SETRANGE SHUTDOWN SINTER SINTERCARD SINTERSTORE SISMEMBER
	SLOWLOG SMEMBERS SMISMEMBER SMOVE SORT SORT_RO SPOP SRANDMEMBER SREM SSCAN STRLEN SUBSCRIBE SUNION SUNIONSTORE SWAPDB TIME TOUCH TTL
	TYPE UNLINK UNSUBSCRIBE UNWATCH WAIT WATCH XACK XADD XAUTOCLAIM XCLAIM XDEL XGROUP XINFO XLEN XPENDING XRANGE XREAD XREADGROUP
	XREVRANGE XTRIM ZADD ZCARD ZCOUNT ZDIFF ZINCRBY ZINTER ZINTERCARD ZINTERSTORE ZLEXCOUNT ZMPOP ZMSCORE ZPOPMAX ZPOPMIN ZRANDMEMBER
	ZRANGE ZRANGEBYLEX ZRANGEBYSCORE ZRANGESTORE ZRANK ZREM ZREMRANGEBYLEX ZREMRANGEBYRANK ZREMRANGEBYSCORE ZREVRANGE ZREVRANK ZSCAN
	ZSCORE ZUNION ZUNIONSTORE JSON.GET JSON.SET JSON.DEL JSON.TYPE JSON.ARRAPPEND JSON.OBJKEYS`)

// commandIndex keeps the commands each open connection told, for completion. It is read from the goroutine of a completion provider,
// so it is guarded.
type commandIndex struct {
	mu   sync.Mutex
	docs map[string][]CommandDoc
	// asked says that the connection's commands are being fetched or were.
	asked map[string]bool
	// target is the connection the tabs show and bound the connection chosen for each .redis file (by URI): copies of what the UI
	// thread holds, for the completion provider.
	target string
	bound  map[string]string
}

func newCommandIndex() *commandIndex {
	return &commandIndex{docs: map[string][]CommandDoc{}, asked: map[string]bool{}, bound: map[string]string{}}
}

// builtinDocs are the built-in names as commands without a summary.
func builtinDocs() []CommandDoc {
	out := make([]CommandDoc, len(builtinCommandNames))
	for i, n := range builtinCommandNames {
		out[i] = CommandDoc{Name: n}
	}
	return out
}

// commandDocs returns the commands of a connection: the server's, else the built-in ones.
func (x *Extension) commandDocs(conn string) []CommandDoc {
	x.cmds.mu.Lock()
	defer x.cmds.mu.Unlock()
	if d := x.cmds.docs[conn]; len(d) > 0 {
		return d
	}
	return builtinDocs()
}

// commandNames are the names of the commands of a connection.
func (x *Extension) commandNames(conn string) []string {
	docs := x.commandDocs(conn)
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.Name
	}
	return out
}

// ensureCommandDocs fetches the commands of a connection once (COMMAND DOCS, else COMMAND), off the UI thread; the console completes
// with the built-in names meanwhile.
func (x *Extension) ensureCommandDocs(conn string) {
	x.cmds.mu.Lock()
	if x.cmds.asked[conn] {
		x.cmds.mu.Unlock()
		return
	}
	x.cmds.asked[conn] = true
	x.cmds.mu.Unlock()
	index := x.consoleDB(conn) // read on the UI thread: the consoles belong to it
	go func() {
		ctx, cancel := context.WithTimeout(x.ctx, 20*time.Second)
		defer cancel()
		db, _, err := x.drv.database(conn, index)
		if err != nil {
			return
		}
		docs := x.fetchCommandDocs(ctx, db)
		if len(docs) == 0 {
			return
		}
		x.cmds.mu.Lock()
		x.cmds.docs[conn] = docs
		x.cmds.mu.Unlock()
		x.api.Post(x.refreshConsole)
	}()
}

// fetchCommandDocs asks the server for its commands: COMMAND DOCS (Redis 7), else COMMAND. A server that answers neither gives none.
func (x *Extension) fetchCommandDocs(ctx context.Context, db DB) []CommandDoc {
	if rep, err := db.Do(ctx, []string{"COMMAND", "DOCS"}); err == nil && !rep.IsError() {
		if docs := ParseCommandDocs(rep); len(docs) > 0 {
			return docs
		}
	}
	if rep, err := db.Do(ctx, []string{"COMMAND"}); err == nil && !rep.IsError() {
		return ParseCommandList(rep)
	}
	return nil
}

// forgetCommandDocs drops what a connection told when it closes.
func (x *Extension) forgetCommandDocs(conn string) {
	x.cmds.mu.Lock()
	delete(x.cmds.docs, conn)
	delete(x.cmds.asked, conn)
	x.cmds.mu.Unlock()
}

// consoleDB is the database the console of a connection uses.
func (x *Extension) consoleDB(conn string) int {
	if c, ok := x.consoles[conn]; ok {
		return c.db
	}
	if s, err := x.drv.session(conn); err == nil {
		return s.dbIndex
	}
	return 0
}
