package redis

import (
	"context"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// ValuePageSize is how many rows of a hash, list, set, sorted set or stream a page of the key editor loads (HSCAN, SSCAN and ZSCAN
// take it as the hint COUNT, LRANGE and XREVRANGE as the length).
const ValuePageSize = 200

// maxPageCalls bounds the scan calls that make one page: HSCAN and its kin may answer with no rows while the cursor is not 0.
const maxPageCalls = 50

// FormField is a field of the small form under a collection (add or edit a row) and of the New Key form.
type FormField struct {
	// Key names the field in the values a form gives back; Label is its caption.
	Key, Label string
	// Placeholder is the hint of an empty field; Value is what it starts with.
	Placeholder, Value string
	// Multiline makes a text area; Choices makes a select box (Value is one of them); Disabled shows the value and refuses edits.
	Multiline bool
	Choices   []string
	Disabled  bool
	// Required makes an empty value an error.
	Required bool
}

// Collection is the rows of a hash, list, set, sorted set or stream as the key editor shows them: a grid source that is loaded in
// pages, and the changes that can be made to it. Fetch, Add, Edit and Remove talk to the server and are called from a goroutine,
// one at a time; Len and Row (the grid.Source) may be called from the UI thread meanwhile.
type Collection interface {
	resultgrid.Source
	// Type is the Redis type of the key.
	Type() string
	// More tells whether there are rows that are not loaded.
	More() bool
	// Fetch loads the next page, or the first, and returns how many rows came. It returns at once when everything is loaded.
	Fetch(ctx context.Context) (int, error)
	// Reset forgets the loaded rows, so that the next Fetch starts again.
	Reset()
	// AddNoun names a row for the buttons ("Field", "Item", "Member", "Entry"); CanEdit tells whether a row can be changed in place.
	AddNoun() string
	CanEdit() bool
	// AddForm is the form that adds a row; EditForm is the form that changes the row i (a row of the source) with its values, or an
	// error when it cannot be edited.
	AddForm() []FormField
	EditForm(i int) ([]FormField, error)
	// Add applies the values of AddForm, and Edit those of EditForm.
	Add(ctx context.Context, values []string) error
	Edit(ctx context.Context, i int, values []string) error
	// Remove deletes the rows (rows of the source).
	Remove(ctx context.Context, rows []int) error
	// NeedsReload tells whether the rows must be loaded again after Add or Remove (Reset and Fetch): a list, whose indexes move, and
	// a stream, whose new entries come first. The others change their rows in place.
	NeedsReload() bool
}

// NewCollection makes the collection of a key of a type, or fails with RedisTypeNotCollection.
func NewCollection(db DB, key, typ string) (Collection, error) {
	switch typ {
	case TypeHash:
		return &hashColl{base: base{db: db, key: key}, index: map[string]int{}}, nil
	case TypeList:
		return &listColl{base: base{db: db, key: key}}, nil
	case TypeSet:
		return &setColl{base: base{db: db, key: key}, index: map[string]int{}}, nil
	case TypeZSet:
		return &zsetColl{base: base{db: db, key: key}, index: map[string]int{}}, nil
	case TypeStream:
		return &streamColl{base: base{db: db, key: key}, max: "+"}, nil
	}
	return nil, errors.New("RedisTypeNotCollection", "this type has no rows", "type", typ)
}

// base is what the collections share: the database, the key, and the lock that guards the rows.
type base struct {
	db  DB
	key string

	mu   sync.RWMutex
	done bool
	// started says that a page was fetched.
	started bool
}

// More implements Collection.
func (b *base) More() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return !b.started || !b.done
}

// cellText makes the value of a cell from the bytes of a Redis value: text, or binary data when it is not text.
func cellText(s string) resultgrid.Value {
	if IsBinary(s) {
		return resultgrid.Bytes([]byte(s))
	}
	return resultgrid.Text(s)
}

// requireText fails for a value the form cannot hold (binary data).
func requireText(what, s string) error {
	if IsBinary(s) {
		return errors.New("RedisRowNotEditable", "binary data cannot be edited here", "what", what)
	}
	return nil
}

// need reads a required field of a form's values.
func need(values []string, i int, name string) (string, error) {
	if i >= len(values) || strings.TrimSpace(values[i]) == "" {
		return "", errors.New("RedisFieldMissing", name+" is required", "field", name)
	}
	return values[i], nil
}

// ---- hash ----

type hashColl struct {
	base
	rows   []Pair
	index  map[string]int
	cursor uint64
}

var hashCols = []resultgrid.Column{{Name: "Field"}, {Name: "Value"}}

func (h *hashColl) Type() string                 { return TypeHash }
func (h *hashColl) Columns() []resultgrid.Column { return hashCols }
func (h *hashColl) AddNoun() string              { return "Field" }
func (h *hashColl) CanEdit() bool                { return true }
func (h *hashColl) NeedsReload() bool            { return false }
func (h *hashColl) Len() int                     { h.mu.RLock(); defer h.mu.RUnlock(); return len(h.rows) }

func (h *hashColl) Row(i int, dst []resultgrid.Value) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	dst[0], dst[1] = cellText(h.rows[i].Field), cellText(h.rows[i].Value)
}

func (h *hashColl) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rows, h.index, h.cursor, h.done, h.started = nil, map[string]int{}, 0, false, false
}

func (h *hashColl) Fetch(ctx context.Context) (int, error) {
	added := 0
	for calls := 0; calls < maxPageCalls && added < ValuePageSize; calls++ {
		h.mu.RLock()
		cursor, done := h.cursor, h.done && h.started
		h.mu.RUnlock()
		if done {
			break
		}
		fields, next, err := h.db.HScan(ctx, h.key, cursor, ValuePageSize)
		if err != nil {
			return added, err
		}
		h.mu.Lock()
		for _, f := range fields {
			if _, seen := h.index[f.Field]; !seen {
				h.index[f.Field] = len(h.rows)
				h.rows = append(h.rows, f)
				added++
			}
		}
		h.cursor, h.started, h.done = next, true, next == 0
		h.mu.Unlock()
	}
	return added, nil
}

func (h *hashColl) AddForm() []FormField {
	return []FormField{{Key: "field", Label: "Field", Required: true}, {Key: "value", Label: "Value", Multiline: true}}
}

func (h *hashColl) EditForm(i int) ([]FormField, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if i < 0 || i >= len(h.rows) {
		return nil, errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	r := h.rows[i]
	if err := requireText("hash value", r.Value); err != nil {
		return nil, err
	}
	return []FormField{{Key: "field", Label: "Field", Value: r.Field, Disabled: true}, {Key: "value", Label: "Value", Value: r.Value, Multiline: true}}, nil
}

func (h *hashColl) Add(ctx context.Context, values []string) error {
	field, err := need(values, 0, "Field")
	if err != nil {
		return err
	}
	value := ""
	if len(values) > 1 {
		value = values[1]
	}
	if _, err := h.db.HSet(ctx, h.key, field, value); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if i, seen := h.index[field]; seen {
		h.rows[i].Value = value
	} else {
		h.index[field] = len(h.rows)
		h.rows = append(h.rows, Pair{field, value})
	}
	return nil
}

func (h *hashColl) Edit(ctx context.Context, i int, values []string) error {
	h.mu.RLock()
	if i < 0 || i >= len(h.rows) {
		h.mu.RUnlock()
		return errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	field := h.rows[i].Field
	h.mu.RUnlock()
	value := ""
	if len(values) > 1 {
		value = values[1]
	}
	if _, err := h.db.HSet(ctx, h.key, field, value); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if j, ok := h.index[field]; ok {
		h.rows[j].Value = value
	}
	return nil
}

func (h *hashColl) Remove(ctx context.Context, rows []int) error {
	h.mu.RLock()
	var fields []string
	for _, i := range rows {
		if i >= 0 && i < len(h.rows) {
			fields = append(fields, h.rows[i].Field)
		}
	}
	h.mu.RUnlock()
	if len(fields) == 0 {
		return errors.New("RedisRowNotFound", "no row to remove")
	}
	if err := h.db.HDel(ctx, h.key, fields...); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	gone := map[string]bool{}
	for _, f := range fields {
		gone[f] = true
	}
	kept := h.rows[:0]
	for _, r := range h.rows {
		if !gone[r.Field] {
			kept = append(kept, r)
		}
	}
	h.rows = kept
	h.index = make(map[string]int, len(kept))
	for i, r := range kept {
		h.index[r.Field] = i
	}
	return nil
}

// ---- list ----

type listColl struct {
	base
	rows []string
}

var listCols = []resultgrid.Column{{Name: "Index", Type: "integer"}, {Name: "Value"}}

func (l *listColl) Type() string                 { return TypeList }
func (l *listColl) Columns() []resultgrid.Column { return listCols }
func (l *listColl) AddNoun() string              { return "Item" }
func (l *listColl) CanEdit() bool                { return true }
func (l *listColl) NeedsReload() bool            { return true }
func (l *listColl) Len() int                     { l.mu.RLock(); defer l.mu.RUnlock(); return len(l.rows) }

func (l *listColl) Row(i int, dst []resultgrid.Value) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	dst[0], dst[1] = resultgrid.Int(int64(i)), cellText(l.rows[i])
}

func (l *listColl) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows, l.done, l.started = nil, false, false
}

func (l *listColl) Fetch(ctx context.Context) (int, error) {
	l.mu.RLock()
	start, done := int64(len(l.rows)), l.done && l.started
	l.mu.RUnlock()
	if done {
		return 0, nil
	}
	items, err := l.db.LRange(ctx, l.key, start, start+ValuePageSize-1)
	if err != nil {
		return 0, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if int64(len(l.rows)) != start {
		return 0, nil // the list was changed while the page was read
	}
	l.rows = append(l.rows, items...)
	l.started, l.done = true, len(items) < ValuePageSize
	return len(items), nil
}

func (l *listColl) AddForm() []FormField {
	return []FormField{{Key: "value", Label: "Value", Multiline: true}, {Key: "where", Label: "Add at", Choices: []string{"tail", "head"}, Value: "tail"}}
}

func (l *listColl) EditForm(i int) ([]FormField, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if i < 0 || i >= len(l.rows) {
		return nil, errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	if err := requireText("list item", l.rows[i]); err != nil {
		return nil, err
	}
	return []FormField{{Key: "value", Label: "Value of item " + strconv.Itoa(i), Value: l.rows[i], Multiline: true}}, nil
}

// Add pushes the value. The rows are loaded again by the caller (Reset and Fetch): a push at the head moves every index.
func (l *listColl) Add(ctx context.Context, values []string) error {
	value := ""
	if len(values) > 0 {
		value = values[0]
	}
	if len(values) > 1 && values[1] == "head" {
		return l.db.LPush(ctx, l.key, value)
	}
	return l.db.RPush(ctx, l.key, value)
}

func (l *listColl) Edit(ctx context.Context, i int, values []string) error {
	l.mu.RLock()
	n := len(l.rows)
	l.mu.RUnlock()
	if i < 0 || i >= n {
		return errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	value := ""
	if len(values) > 0 {
		value = values[0]
	}
	if err := l.db.LSet(ctx, l.key, int64(i), value); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if i < len(l.rows) {
		l.rows[i] = value
	}
	return nil
}

// Remove deletes the items from the last to the first so that the indexes of the others stay right. The rows are loaded again by
// the caller.
func (l *listColl) Remove(ctx context.Context, rows []int) error {
	if len(rows) == 0 {
		return errors.New("RedisRowNotFound", "no row to remove")
	}
	sorted := append([]int(nil), rows...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] > sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	for _, i := range sorted {
		if err := l.db.LRemoveAt(ctx, l.key, int64(i)); err != nil {
			return err
		}
	}
	return nil
}

// ---- set ----

type setColl struct {
	base
	rows   []string
	index  map[string]int
	cursor uint64
}

var setCols = []resultgrid.Column{{Name: "Member"}}

func (s *setColl) Type() string                 { return TypeSet }
func (s *setColl) Columns() []resultgrid.Column { return setCols }
func (s *setColl) AddNoun() string              { return "Member" }
func (s *setColl) CanEdit() bool                { return false }
func (s *setColl) NeedsReload() bool            { return false }
func (s *setColl) Len() int                     { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.rows) }

func (s *setColl) Row(i int, dst []resultgrid.Value) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dst[0] = cellText(s.rows[i])
}

func (s *setColl) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows, s.index, s.cursor, s.done, s.started = nil, map[string]int{}, 0, false, false
}

func (s *setColl) Fetch(ctx context.Context) (int, error) {
	added := 0
	for calls := 0; calls < maxPageCalls && added < ValuePageSize; calls++ {
		s.mu.RLock()
		cursor, done := s.cursor, s.done && s.started
		s.mu.RUnlock()
		if done {
			break
		}
		members, next, err := s.db.SScan(ctx, s.key, cursor, ValuePageSize)
		if err != nil {
			return added, err
		}
		s.mu.Lock()
		for _, m := range members {
			if _, seen := s.index[m]; !seen {
				s.index[m] = len(s.rows)
				s.rows = append(s.rows, m)
				added++
			}
		}
		s.cursor, s.started, s.done = next, true, next == 0
		s.mu.Unlock()
	}
	return added, nil
}

func (s *setColl) AddForm() []FormField {
	return []FormField{{Key: "member", Label: "Member", Required: true, Multiline: true}}
}

func (s *setColl) EditForm(int) ([]FormField, error) {
	return nil, errors.New("RedisRowNotEditable", "a member of a set is added and removed, not changed")
}

func (s *setColl) Add(ctx context.Context, values []string) error {
	member, err := need(values, 0, "Member")
	if err != nil {
		return err
	}
	if _, err := s.db.SAdd(ctx, s.key, member); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, seen := s.index[member]; !seen {
		s.index[member] = len(s.rows)
		s.rows = append(s.rows, member)
	}
	return nil
}

func (s *setColl) Edit(_ context.Context, i int, _ []string) error {
	if i < 0 || i >= s.Len() {
		return errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	return errors.New("RedisRowNotEditable", "a member of a set is added and removed, not changed")
}

func (s *setColl) Remove(ctx context.Context, rows []int) error {
	s.mu.RLock()
	var members []string
	for _, i := range rows {
		if i >= 0 && i < len(s.rows) {
			members = append(members, s.rows[i])
		}
	}
	s.mu.RUnlock()
	if len(members) == 0 {
		return errors.New("RedisRowNotFound", "no row to remove")
	}
	if err := s.db.SRem(ctx, s.key, members...); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	gone := map[string]bool{}
	for _, m := range members {
		gone[m] = true
	}
	kept := s.rows[:0]
	for _, m := range s.rows {
		if !gone[m] {
			kept = append(kept, m)
		}
	}
	s.rows = kept
	s.index = make(map[string]int, len(kept))
	for i, m := range kept {
		s.index[m] = i
	}
	return nil
}

// ---- sorted set ----

type zsetColl struct {
	base
	rows   []ZMember
	index  map[string]int
	cursor uint64
}

var zsetCols = []resultgrid.Column{{Name: "Member"}, {Name: "Score", Type: "double"}}

func (z *zsetColl) Type() string                 { return TypeZSet }
func (z *zsetColl) Columns() []resultgrid.Column { return zsetCols }
func (z *zsetColl) AddNoun() string              { return "Member" }
func (z *zsetColl) CanEdit() bool                { return true }
func (z *zsetColl) NeedsReload() bool            { return false }
func (z *zsetColl) Len() int                     { z.mu.RLock(); defer z.mu.RUnlock(); return len(z.rows) }

func (z *zsetColl) Row(i int, dst []resultgrid.Value) {
	z.mu.RLock()
	defer z.mu.RUnlock()
	dst[0], dst[1] = cellText(z.rows[i].Member), resultgrid.Float(z.rows[i].Score)
}

func (z *zsetColl) Reset() {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.rows, z.index, z.cursor, z.done, z.started = nil, map[string]int{}, 0, false, false
}

func (z *zsetColl) Fetch(ctx context.Context) (int, error) {
	added := 0
	for calls := 0; calls < maxPageCalls && added < ValuePageSize; calls++ {
		z.mu.RLock()
		cursor, done := z.cursor, z.done && z.started
		z.mu.RUnlock()
		if done {
			break
		}
		members, next, err := z.db.ZScan(ctx, z.key, cursor, ValuePageSize)
		if err != nil {
			return added, err
		}
		z.mu.Lock()
		for _, m := range members {
			if _, seen := z.index[m.Member]; !seen {
				z.index[m.Member] = len(z.rows)
				z.rows = append(z.rows, m)
				added++
			}
		}
		z.cursor, z.started, z.done = next, true, next == 0
		z.mu.Unlock()
	}
	return added, nil
}

func (z *zsetColl) AddForm() []FormField {
	return []FormField{{Key: "member", Label: "Member", Required: true, Multiline: true}, {Key: "score", Label: "Score", Placeholder: "0", Required: true}}
}

func (z *zsetColl) EditForm(i int) ([]FormField, error) {
	z.mu.RLock()
	defer z.mu.RUnlock()
	if i < 0 || i >= len(z.rows) {
		return nil, errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	r := z.rows[i]
	if err := requireText("sorted set member", r.Member); err != nil {
		return nil, err
	}
	return []FormField{{Key: "member", Label: "Member", Value: r.Member, Disabled: true, Multiline: true},
		{Key: "score", Label: "Score", Value: FormatScore(r.Score), Required: true}}, nil
}

// FormatScore writes a score the way Redis does: the shortest text that reads back as the same number, "inf" and "-inf".
func FormatScore(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// ParseScore reads a score: a number, or inf, +inf and -inf.
func ParseScore(s string) (float64, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "inf", "+inf":
		return math.Inf(1), nil
	case "-inf":
		return math.Inf(-1), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) {
		return 0, errors.New("RedisScoreInvalid", "a score is a number, inf or -inf", "score", s)
	}
	return f, nil
}

func (z *zsetColl) Add(ctx context.Context, values []string) error {
	member, err := need(values, 0, "Member")
	if err != nil {
		return err
	}
	scoreText, err := need(values, 1, "Score")
	if err != nil {
		return err
	}
	score, err := ParseScore(scoreText)
	if err != nil {
		return err
	}
	if _, err := z.db.ZAdd(ctx, z.key, member, score); err != nil {
		return err
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if i, seen := z.index[member]; seen {
		z.rows[i].Score = score
	} else {
		z.index[member] = len(z.rows)
		z.rows = append(z.rows, ZMember{member, score})
	}
	return nil
}

func (z *zsetColl) Edit(ctx context.Context, i int, values []string) error {
	z.mu.RLock()
	if i < 0 || i >= len(z.rows) {
		z.mu.RUnlock()
		return errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	member := z.rows[i].Member
	z.mu.RUnlock()
	scoreText, err := need(values, 1, "Score")
	if err != nil {
		return err
	}
	score, err := ParseScore(scoreText)
	if err != nil {
		return err
	}
	if _, err := z.db.ZAdd(ctx, z.key, member, score); err != nil {
		return err
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	if j, ok := z.index[member]; ok {
		z.rows[j].Score = score
	}
	return nil
}

func (z *zsetColl) Remove(ctx context.Context, rows []int) error {
	z.mu.RLock()
	var members []string
	for _, i := range rows {
		if i >= 0 && i < len(z.rows) {
			members = append(members, z.rows[i].Member)
		}
	}
	z.mu.RUnlock()
	if len(members) == 0 {
		return errors.New("RedisRowNotFound", "no row to remove")
	}
	if err := z.db.ZRem(ctx, z.key, members...); err != nil {
		return err
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	gone := map[string]bool{}
	for _, m := range members {
		gone[m] = true
	}
	kept := z.rows[:0]
	for _, r := range z.rows {
		if !gone[r.Member] {
			kept = append(kept, r)
		}
	}
	z.rows = kept
	z.index = make(map[string]int, len(kept))
	for i, r := range kept {
		z.index[r.Member] = i
	}
	return nil
}

// ---- stream ----

type streamColl struct {
	base
	rows []StreamEntry
	// max is the largest id the next page reads: "+" for the first, then the id before the oldest entry loaded.
	max string
}

var streamCols = []resultgrid.Column{{Name: "ID"}, {Name: "Time", Type: "timestamp"}, {Name: "Fields"}}

func (s *streamColl) Type() string                 { return TypeStream }
func (s *streamColl) Columns() []resultgrid.Column { return streamCols }
func (s *streamColl) AddNoun() string              { return "Entry" }
func (s *streamColl) CanEdit() bool                { return false }
func (s *streamColl) NeedsReload() bool            { return true }
func (s *streamColl) Len() int                     { s.mu.RLock(); defer s.mu.RUnlock(); return len(s.rows) }

func (s *streamColl) Row(i int, dst []resultgrid.Value) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e := s.rows[i]
	dst[0] = resultgrid.Text(e.ID)
	if ms, _, ok := splitStreamID(e.ID); ok {
		dst[1] = resultgrid.Time(time.UnixMilli(int64(ms)).UTC())
	}
	dst[2] = cellText(FormatEntryFields(e.Fields))
}

// FormatEntryFields writes the fields of a stream entry on one line: field=value field=value.
func FormatEntryFields(fields []Pair) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(f.Field)
		b.WriteByte('=')
		b.WriteString(f.Value)
	}
	return b.String()
}

func (s *streamColl) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows, s.max, s.done, s.started = nil, "+", false, false
}

func (s *streamColl) Fetch(ctx context.Context) (int, error) {
	s.mu.RLock()
	max, done := s.max, s.done && s.started
	s.mu.RUnlock()
	if done {
		return 0, nil
	}
	entries, err := s.db.XRevRange(ctx, s.key, max, ValuePageSize)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, entries...)
	s.started = true
	switch {
	case len(entries) < ValuePageSize:
		s.done = true
	default:
		before, ok := idBefore(entries[len(entries)-1].ID)
		if !ok {
			s.done = true
		}
		s.max = before
	}
	return len(entries), nil
}

// splitStreamID reads the two numbers of an id, "1700000000000-3".
func splitStreamID(id string) (ms, seq uint64, ok bool) {
	a, b, found := strings.Cut(id, "-")
	if !found {
		b = "0"
	}
	ms, err := strconv.ParseUint(a, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	seq, err = strconv.ParseUint(b, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return ms, seq, true
}

// idBefore is the id right before id, which the next page of XREVRANGE ends with; ok is false for the first id there is (0-0).
func idBefore(id string) (string, bool) {
	ms, seq, ok := splitStreamID(id)
	switch {
	case !ok:
		return "", false
	case seq > 0:
		return strconv.FormatUint(ms, 10) + "-" + strconv.FormatUint(seq-1, 10), true
	case ms > 0:
		return strconv.FormatUint(ms-1, 10) + "-" + strconv.FormatUint(math.MaxUint64, 10), true
	}
	return "", false
}

func (s *streamColl) AddForm() []FormField {
	return []FormField{
		{Key: "id", Label: "ID", Placeholder: "* (the server makes it)"},
		{Key: "fields", Label: "Fields (one per line: name, a space, value)", Multiline: true, Required: true, Placeholder: "temperature 21.5\nhumidity 40"},
	}
}

func (s *streamColl) EditForm(int) ([]FormField, error) {
	return nil, errors.New("RedisRowNotEditable", "an entry of a stream is added and deleted, not changed")
}

// ParseEntryFields reads the fields of a stream entry written one per line, as the name, a space and the value.
func ParseEntryFields(text string) ([]Pair, error) {
	var out []Pair
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, _ := strings.Cut(strings.TrimLeft(line, " \t"), " ")
		out = append(out, Pair{Field: name, Value: value})
	}
	if len(out) == 0 {
		return nil, errors.New("RedisStreamFieldsInvalid", "an entry needs at least one field")
	}
	return out, nil
}

func (s *streamColl) Add(ctx context.Context, values []string) error {
	id := "*"
	if len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		id = strings.TrimSpace(values[0])
	}
	text, err := need(values, 1, "Fields")
	if err != nil {
		return err
	}
	fields, err := ParseEntryFields(text)
	if err != nil {
		return err
	}
	_, err = s.db.XAdd(ctx, s.key, id, fields)
	return err
}

func (s *streamColl) Edit(_ context.Context, i int, _ []string) error {
	if i < 0 || i >= s.Len() {
		return errors.New("RedisRowNotFound", "there is no such row", "row", i)
	}
	return errors.New("RedisRowNotEditable", "an entry of a stream is added and deleted, not changed")
}

func (s *streamColl) Remove(ctx context.Context, rows []int) error {
	s.mu.RLock()
	var ids []string
	for _, i := range rows {
		if i >= 0 && i < len(s.rows) {
			ids = append(ids, s.rows[i].ID)
		}
	}
	s.mu.RUnlock()
	if len(ids) == 0 {
		return errors.New("RedisRowNotFound", "no row to remove")
	}
	return s.db.XDel(ctx, s.key, ids...)
}
