package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rm4n0s/errors"
)

// KeyType describes a type of key to the explorer and the New Key form.
type KeyType struct {
	// Type is the name TYPE gives it; Label is what the form shows; Icon is the Codicon of its keys in the explorer.
	Type, Label, Icon string
}

// KeyTypes are the types the key editor can show, in the order of the New Key form.
var KeyTypes = []KeyType{
	{TypeString, "String", "symbol-string"},
	{TypeHash, "Hash", "symbol-namespace"},
	{TypeList, "List", "list-unordered"},
	{TypeSet, "Set", "symbol-enum"},
	{TypeZSet, "Sorted set", "list-ordered"},
	{TypeStream, "Stream", "pulse"},
	{TypeJSON, "JSON (ReJSON)", "bracket-dot"},
}

// IconOf is the Codicon of the keys of a type; keys of a type the editor does not know get a plain one.
func IconOf(typ string) string {
	for _, k := range KeyTypes {
		if k.Type == typ {
			return k.Icon
		}
	}
	return "symbol-misc"
}

// LabelOf is the name of a type for the user.
func LabelOf(typ string) string {
	for _, k := range KeyTypes {
		if k.Type == typ {
			return k.Label
		}
	}
	return typ
}

// NewKeyFields are the fields of the New Key form below the name and the type: the first value of a key of the type. The ones that
// add a row to a collection are those of its AddForm, with the position of a list left out (a new list has no head or tail yet).
func NewKeyFields(typ string) []FormField {
	switch typ {
	case TypeString:
		return []FormField{{Key: "value", Label: "Value", Multiline: true}}
	case TypeList:
		return []FormField{{Key: "value", Label: "First item", Multiline: true}}
	case TypeJSON:
		return []FormField{{Key: "value", Label: "JSON", Multiline: true, Placeholder: `{"name": "value"}`, Required: true}}
	}
	coll, err := NewCollection(nil, "", typ)
	if err != nil {
		return nil
	}
	return coll.AddForm()
}

// CreateKey makes a key of a type with its first value; values are those of NewKeyFields. A key that exists is not touched
// (RedisKeyExists). ttl above zero gives the key a time to live. The errors also have the tags of Collection.Add, RedisNameMissing,
// RedisNewJSONInvalid and RedisTypeUnknown.
func CreateKey(ctx context.Context, db DB, key, typ string, values []string, ttl time.Duration) error {
	if key == "" {
		return errors.New("RedisNameMissing", "a key needs a name")
	}
	switch typ {
	case TypeString, TypeHash, TypeList, TypeSet, TypeZSet, TypeStream, TypeJSON:
	default:
		return errors.New("RedisTypeUnknown", "no key of this type can be made here", "type", typ)
	}
	// the first value is checked before anything is written, so that a mistake leaves no empty key behind
	if typ == TypeJSON {
		if len(values) == 0 || !json.Valid([]byte(values[0])) {
			return errors.New("RedisNewJSONInvalid", "the text is not valid JSON")
		}
	}
	exists, err := db.Exists(ctx, key)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("RedisKeyExists", "a key with this name exists")
	}
	switch typ {
	case TypeString:
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		err = db.Set(ctx, key, value)
	case TypeJSON:
		err = db.JSONSet(ctx, key, values[0])
	default:
		var coll Collection
		coll, err = NewCollection(db, key, typ)
		if err == nil {
			err = coll.Add(ctx, values)
		}
	}
	if err != nil {
		return err
	}
	if ttl > 0 {
		return db.Expire(ctx, key, ttl)
	}
	return nil
}
