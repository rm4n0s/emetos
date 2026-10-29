package redis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// MaxStringBytes is how much of a string the key editor loads (SPEC §10.23 "The key editor": big strings are cut at 1 MiB with a
// note). A string that is longer is shown read-only.
const MaxStringBytes = 1 << 20

// StringValue is the value of a string key, or of a ReJSON key, as the editor holds it.
type StringValue struct {
	// Data is the value, cut at MaxStringBytes without breaking a UTF-8 sequence.
	Data string
	// Size is the length of the whole value in bytes; Truncated says that Data is only its beginning.
	Size      int64
	Truncated bool
}

// LoadString reads a string key.
func LoadString(ctx context.Context, db DB, key string) (StringValue, error) {
	data, size, err := db.GetRange(ctx, key, MaxStringBytes)
	if err != nil {
		return StringValue{}, err
	}
	v := StringValue{Data: data, Size: size, Truncated: size > int64(len(data))}
	if v.Truncated {
		v.Data = cutUTF8(data)
	}
	return v, nil
}

// cutUTF8 drops an incomplete UTF-8 sequence at the end of s, when s is text.
func cutUTF8(s string) string {
	for i := 0; i < utf8.UTFMax && i < len(s); i++ {
		r, size := utf8.DecodeLastRuneInString(s[:len(s)-i])
		if r != utf8.RuneError || size != 1 {
			return s[:len(s)-i]
		}
	}
	return s
}

// Binary tells whether the value is not text: it is not valid UTF-8, or it holds a NUL byte. The editor shows it as hex and does not
// edit it.
func (v StringValue) Binary() bool { return IsBinary(v.Data) }

// IsBinary tells whether s is not text.
func IsBinary(s string) bool { return !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 }

// PrettyJSON returns the value as indented JSON when it is a JSON object or array (a number or a quoted string is not shown as JSON:
// it is text).
func (v StringValue) PrettyJSON() (string, bool) {
	if v.Truncated || v.Binary() {
		return "", false
	}
	trimmed := strings.TrimSpace(v.Data)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return "", false
	}
	return IndentJSON(trimmed)
}

// IndentJSON indents a JSON text with two spaces; ok is false when it is not valid JSON.
func IndentJSON(s string) (string, bool) {
	if !json.Valid([]byte(s)) {
		return "", false
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(s), "", "  "); err != nil {
		return "", false
	}
	return out.String(), true
}

// JSONForSave prepares the text of the JSON view for saving. The text must be valid JSON. A value that was stored compact stays
// compact (the view indents it only to be read); one that was stored indented is saved as it is shown.
func JSONForSave(original, edited string) (string, error) {
	if !json.Valid([]byte(edited)) {
		return "", errors.New("RedisJSONInvalid", "the text is not valid JSON")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(strings.TrimSpace(original))); err == nil && compact.String() == strings.TrimSpace(original) {
		compact.Reset()
		if err := json.Compact(&compact, []byte(edited)); err != nil {
			return edited, nil // valid JSON that cannot be compacted: it is saved as it is written
		}
		return compact.String(), nil
	}
	return edited, nil
}

// SaveString writes a string key, keeping its TTL. A value that was cut when it was loaded must not be saved: the rest would be lost.
func SaveString(ctx context.Context, db DB, key string, loaded StringValue, text string) error {
	if loaded.Truncated {
		return errors.New("RedisValueTruncated", "the value was cut when it was loaded and cannot be saved", "size", loaded.Size)
	}
	return db.Set(ctx, key, text)
}

// HexDump writes data in the layout of hexdump -C: the offset, sixteen bytes in hex and the same bytes as characters.
func HexDump(data string) string {
	var b strings.Builder
	for off := 0; off < len(data); off += 16 {
		end := min(off+16, len(data))
		fmt.Fprintf(&b, "%08x  ", off)
		for i := off; i < off+16; i++ {
			if i < end {
				fmt.Fprintf(&b, "%02x ", data[i])
			} else {
				b.WriteString("   ")
			}
			if i == off+7 {
				b.WriteByte(' ')
			}
		}
		b.WriteString(" |")
		for i := off; i < end; i++ {
			if c := data[i]; c >= 0x20 && c < 0x7f {
				b.WriteByte(c)
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteString("|\n")
	}
	return b.String()
}

// LoadJSON reads a ReJSON key as indented JSON text and as it is stored.
func LoadJSON(ctx context.Context, db DB, key string) (stored, pretty string, err error) {
	stored, err = db.JSONGet(ctx, key)
	if err != nil {
		return "", "", err
	}
	if p, ok := IndentJSON(stored); ok {
		return stored, p, nil
	}
	return stored, stored, nil
}

// SaveJSON writes a ReJSON key from the text of the view.
func SaveJSON(ctx context.Context, db DB, key, stored, edited string) error {
	doc, err := JSONForSave(stored, edited)
	if err != nil {
		return err
	}
	return db.JSONSet(ctx, key, doc)
}
