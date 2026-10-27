package protocol

import (
	"bytes"
	"testing"

	"github.com/rm4n0s/errors"
)

// FuzzDecode feeds bytes to Decode and to a Reader. Neither may panic or return an error that is not an *errors.Error,
// whatever is accepted encodes back to the same bytes, and the Reader reads what Decode accepts. Run it as CLAUDE.md says:
//
//	GOMEMLIMIT=2GiB go test -run '^$' -fuzz FuzzDecode -fuzztime=30s -parallel=1 ./extensions/remotework/protocol/
func FuzzDecode(f *testing.F) {
	for _, m := range samples() {
		b, err := Append(nil, m)
		if err != nil {
			f.Fatal(err)
		}
		if len(b) < 70000 { // the seeds stay small; the large clipboard is covered by the table
			f.Add(b)
		}
	}
	f.Add([]byte{})
	f.Add([]byte{byte(TypeClipboard), 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{byte(TypeTile), 0, 0, 0, 28})
	f.Fuzz(func(t *testing.T, in []byte) {
		m, err := Decode(in)
		if err != nil {
			if _, ok := errors.FromError(err); !ok {
				t.Fatalf("the error is %T, not *errors.Error", err)
			}
		} else {
			out, err := Append(nil, m)
			if err != nil || !bytes.Equal(out, in) {
				t.Fatalf("decoded to %+v and encodes to %x (%v)", m, out, err)
			}
		}
		r := NewReader(bytes.NewReader(in))
		for range 4 {
			got, err := r.Next()
			if err != nil {
				if _, ok := errors.FromError(err); !ok {
					t.Fatalf("the reader's error is %T, not *errors.Error", err)
				}
				return
			}
			if m != nil && got.Type() != m.Type() {
				t.Fatalf("the reader read a %v, Decode a %v", got.Type(), m.Type())
			}
			if cap(r.buf) > MaxMessage {
				t.Fatalf("the reader's buffer is %d bytes", cap(r.buf))
			}
		}
	})
}
