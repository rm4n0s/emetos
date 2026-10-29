package redis

import (
	"strings"
)

// commandWords gives the command name and, for a command that has subcommands, the subcommand, both in capitals: "CONFIG", "SET".
// The subcommand is empty when the command has none or the line has no word after it.
func commandWords(args []string) (name, sub string) {
	if len(args) == 0 {
		return "", ""
	}
	name = strings.ToUpper(args[0])
	if len(args) > 1 {
		sub = strings.ToUpper(args[1])
	}
	return name, sub
}

// Dangerous says why a command asks for a confirmation before it runs (SPEC §10.23 "Safety", the setting redis.confirmDangerous):
// FLUSHDB, FLUSHALL, SHUTDOWN, CONFIG SET, DEBUG, KEYS, SCRIPT FLUSH, CLIENT KILL, and DEL of more than one key. It gives the
// sentence the confirmation shows, or "" for a command that runs without asking.
func Dangerous(args []string) string {
	name, sub := commandWords(args)
	switch name {
	case "FLUSHALL":
		return "FLUSHALL deletes every key of every database of the server."
	case "FLUSHDB":
		return "FLUSHDB deletes every key of the database."
	case "SHUTDOWN":
		return "SHUTDOWN stops the server."
	case "DEBUG":
		return "DEBUG is for the developers of the server: some of its subcommands crash it or block it."
	case "KEYS":
		return "KEYS walks every key of the database in one command and blocks the server while it does; the explorer uses SCAN instead."
	case "CONFIG":
		if sub == "SET" {
			return "CONFIG SET changes the configuration of the running server."
		}
	case "SCRIPT":
		if sub == "FLUSH" {
			return "SCRIPT FLUSH removes every script from the cache of the server."
		}
	case "CLIENT":
		if sub == "KILL" {
			return "CLIENT KILL closes the connections of other clients."
		}
	case "DEL":
		if len(args) > 2 {
			return "DEL of several keys deletes all of them."
		}
	}
	return ""
}

// unsupportedReasons say why the console does not send a command: it needs a connection of its own, or it changes the state of the
// connection that the console shares with its other commands (the console sends each command on a connection of a pool).
var unsupportedReasons = map[string]string{
	"SUBSCRIBE":    "SUBSCRIBE needs a connection of its own: use the Redis Pub/Sub tab.",
	"PSUBSCRIBE":   "PSUBSCRIBE needs a connection of its own: use the Redis Pub/Sub tab.",
	"SSUBSCRIBE":   "SSUBSCRIBE needs a connection of its own: use the Redis Pub/Sub tab.",
	"UNSUBSCRIBE":  "UNSUBSCRIBE belongs to the Redis Pub/Sub tab.",
	"PUNSUBSCRIBE": "PUNSUBSCRIBE belongs to the Redis Pub/Sub tab.",
	"SUNSUBSCRIBE": "SUNSUBSCRIBE belongs to the Redis Pub/Sub tab.",
	"MONITOR":      "MONITOR needs a connection of its own: use the Redis Monitor tab.",
	"MULTI":        "The console sends each command on its own, so MULTI would not reach EXEC: use a script (EVAL) for several commands that must run together.",
	"EXEC":         "The console sends each command on its own, so there is no MULTI for EXEC to run.",
	"DISCARD":      "The console sends each command on its own, so there is no MULTI to discard.",
	"WATCH":        "The console sends each command on its own, so a WATCH would not hold for the EXEC after it.",
	"UNWATCH":      "The console sends each command on its own, so there is no WATCH to undo.",
	"HELLO":        "HELLO would change the protocol of one connection of the pool: the connection settings choose it.",
	"AUTH":         "AUTH would change the user of one connection of the pool: the user and the password are in the connection settings.",
	"QUIT":         "QUIT closes a connection: use Disconnect in the Databases view.",
	"RESET":        "RESET would change the state of one connection of the pool.",
	"SYNC":         "SYNC turns a connection into a replica link.",
	"PSYNC":        "PSYNC turns a connection into a replica link.",
}

// Unsupported says why the console does not run a command, or "" when it does. SELECT is not listed: the console runs it itself and
// changes its database.
func Unsupported(args []string) string {
	name, sub := commandWords(args)
	if r, ok := unsupportedReasons[name]; ok {
		return r
	}
	if name == "CLIENT" {
		switch sub {
		case "REPLY", "TRACKING", "CACHING", "NO-EVICT", "NO-TOUCH", "SETINFO", "SETNAME":
			return "CLIENT " + sub + " would change one connection of the pool, not the console."
		}
	}
	return ""
}

// readOnlyAlways are the commands (by name, or by name and subcommand) that a read-only connection refuses whatever the server flags
// them as: they are not "write" commands to the server, but they change the server, its scripts or what other clients receive.
var readOnlyAlways = map[string]bool{
	"EVAL": true, "EVALSHA": true, "FCALL": true, "PUBLISH": true, "SPUBLISH": true,
	"SHUTDOWN": true, "DEBUG": true, "REPLICAOF": true, "SLAVEOF": true, "FAILOVER": true, "BGSAVE": true, "BGREWRITEAOF": true, "SAVE": true,
	"CONFIG SET": true, "CONFIG REWRITE": true, "CONFIG RESETSTAT": true,
	"SCRIPT FLUSH": true, "SCRIPT KILL": true,
	"FUNCTION LOAD": true, "FUNCTION DELETE": true, "FUNCTION FLUSH": true, "FUNCTION RESTORE": true, "FUNCTION KILL": true,
	"CLIENT KILL": true, "CLIENT PAUSE": true, "CLIENT UNPAUSE": true,
	"ACL SETUSER": true, "ACL DELUSER": true, "ACL LOAD": true, "ACL SAVE": true,
	"MODULE LOAD": true, "MODULE LOADEX": true, "MODULE UNLOAD": true,
	"SLOWLOG RESET": true, "MEMORY PURGE": true, "LATENCY RESET": true,
	"CLUSTER RESET": true, "CLUSTER FAILOVER": true, "CLUSTER FORGET": true, "CLUSTER MEET": true, "CLUSTER ADDSLOTS": true,
	"CLUSTER DELSLOTS": true, "CLUSTER SETSLOT": true, "CLUSTER REPLICATE": true, "CLUSTER FLUSHSLOTS": true, "CLUSTER SAVECONFIG": true,
}

// builtinWrites are the write commands the console knows when the server cannot say (COMMAND INFO fails, or does not know the command):
// the read-only wrapper refuses them.
var builtinWrites = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`
		APPEND BITFIELD BITOP BLMOVE BLMPOP BLPOP BRPOP BRPOPLPUSH BZMPOP BZPOPMAX BZPOPMIN COPY DECR DECRBY DEL EXPIRE EXPIREAT
		FLUSHALL FLUSHDB GEOADD GEORADIUS GEORADIUSBYMEMBER GEOSEARCHSTORE GETDEL GETEX GETSET HDEL HEXPIRE HEXPIREAT HGETDEL HGETEX
		HINCRBY HINCRBYFLOAT HMSET HPERSIST HPEXPIRE HPEXPIREAT HSET HSETEX HSETNX INCR INCRBY INCRBYFLOAT LINSERT LMOVE LMPOP LPOP
		LPUSH LPUSHX LREM LSET LTRIM MIGRATE MOVE MSET MSETNX PERSIST PEXPIRE PEXPIREAT PFADD PFMERGE PSETEX RENAME RENAMENX RESTORE
		RESTORE-ASKING RPOP RPOPLPUSH RPUSH RPUSHX SADD SDIFFSTORE SET SETBIT SETEX SETNX SETRANGE SINTERSTORE SMOVE SORT SPOP SREM
		SUNIONSTORE SWAPDB UNLINK XACK XADD XAUTOCLAIM XCLAIM XDEL XGROUP XSETID XTRIM ZADD ZDIFFSTORE ZINCRBY ZINTERSTORE ZMPOP
		ZPOPMAX ZPOPMIN ZRANGESTORE ZREM ZREMRANGEBYLEX ZREMRANGEBYRANK ZREMRANGEBYSCORE ZUNIONSTORE
		JSON.SET JSON.MSET JSON.MERGE JSON.DEL JSON.FORGET JSON.ARRAPPEND JSON.ARRINSERT JSON.ARRPOP JSON.ARRTRIM JSON.CLEAR
		JSON.NUMINCRBY JSON.NUMMULTBY JSON.STRAPPEND JSON.TOGGLE
		TS.ADD TS.MADD TS.CREATE TS.ALTER TS.DEL TS.INCRBY TS.DECRBY TS.CREATERULE TS.DELETERULE
		BF.ADD BF.MADD BF.INSERT BF.RESERVE CF.ADD CF.ADDNX CF.INSERT CF.INSERTNX CF.DEL CF.RESERVE CMS.INCRBY CMS.INITBYDIM CMS.INITBYPROB CMS.MERGE
		TOPK.ADD TOPK.INCRBY TOPK.RESERVE TDIGEST.ADD TDIGEST.CREATE TDIGEST.MERGE TDIGEST.RESET
		FT.CREATE FT.DROPINDEX FT.ALTER FT.ADD FT.DEL FT.ALIASADD FT.ALIASDEL FT.ALIASUPDATE`) {
		m[w] = true
	}
	return m
}()

// builtinWriteSubs are the write subcommands of the commands that have subcommands, for the same use.
var builtinWriteSubs = map[string]bool{
	"XGROUP CREATE": true, "XGROUP DESTROY": true, "XGROUP SETID": true, "XGROUP CREATECONSUMER": true, "XGROUP DELCONSUMER": true,
}

// RefusedByBuiltinList tells whether a read-only connection refuses the command by the lists of the console alone: the commands it
// always refuses (readOnlyAlways), and the write commands it knows (builtinWrites) when the server could not flag them. A write
// the server flags with "write" is refused by the wrapper from COMMAND INFO instead.
func RefusedByBuiltinList(args []string) bool {
	name, sub := commandWords(args)
	if name == "" {
		return false
	}
	if readOnlyAlways[name] || (sub != "" && readOnlyAlways[name+" "+sub]) {
		return true
	}
	if sub != "" && builtinWriteSubs[name+" "+sub] {
		return true
	}
	return builtinWrites[name]
}
