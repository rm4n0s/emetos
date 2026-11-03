package variables

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Dynamic gives the dynamic variables their values. The zero value uses the clock, the system's random source and the process
// environment; the tests replace them.
type Dynamic struct {
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Rand is the source of random bytes; nil is crypto/rand.
	Rand io.Reader
	// Getenv reads the process environment; nil is os.Getenv.
	Getenv func(string) string
	// AllowedProcessEnv are the names {{$processEnv NAME}} may read (the setting httpClient.allowedProcessEnv, empty by default, so a
	// collection cannot read the environment of the user unasked).
	AllowedProcessEnv []string
}

// value returns the value of the dynamic variable (the text between {{ and }}, starting with "$"), and false for one that is not
// known, or that is not allowed.
func (d Dynamic) value(expr string) (string, bool) {
	name, arg, _ := strings.Cut(expr, " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "$guid", "$randomUUID":
		return d.uuid()
	case "$timestamp":
		return strconv.FormatInt(d.now().Unix(), 10), true
	case "$isoTimestamp":
		return d.now().UTC().Format("2006-01-02T15:04:05.000Z"), true
	case "$randomInt":
		var b [8]byte
		if _, err := io.ReadFull(d.random(), b[:]); err != nil {
			return "", false
		}
		return strconv.FormatUint(binary.BigEndian.Uint64(b[:])%1001, 10), true
	case "$processEnv":
		if arg == "" || !slices.Contains(d.AllowedProcessEnv, arg) {
			return "", false
		}
		get := d.Getenv
		if get == nil {
			get = os.Getenv
		}
		return get(arg), true
	}
	return "", false
}

func (d Dynamic) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Dynamic) random() io.Reader {
	if d.Rand != nil {
		return d.Rand
	}
	return rand.Reader
}

// uuid makes a version 4 UUID.
func (d Dynamic) uuid() (string, bool) {
	var b [16]byte
	if _, err := io.ReadFull(d.random(), b[:]); err != nil {
		return "", false
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexDigits[c>>4], hexDigits[c&0x0f])
	}
	return string(out), true
}
