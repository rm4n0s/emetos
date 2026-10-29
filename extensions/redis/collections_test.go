package redis

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/resultgrid"
)

// rowsOf reads every row of a collection as text.
func rowsOf(c Collection) [][]string {
	out := make([][]string, c.Len())
	for i := range out {
		dst := make([]resultgrid.Value, len(c.Columns()))
		c.Row(i, dst)
		row := make([]string, len(dst))
		for j, v := range dst {
			row[j] = v.Full("NULL")
		}
		out[i] = row
	}
	return out
}

func newColl(t *testing.T, s *fakeServer, key string) Collection {
	t.Helper()
	db := mustDB(t, s, 0)
	info, err := db.Info(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCollection(db, key, info.Type)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func fetchAll(t *testing.T, c Collection) {
	t.Helper()
	for i := 0; c.More(); i++ {
		if i > 1000 {
			t.Fatal("the collection never ends")
		}
		if _, err := c.Fetch(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEachTypeShowsItsRows(t *testing.T) {
	tests := []struct {
		key  string
		cols []string
		rows [][]string
	}{
		{"user:42:profile", []string{"Field", "Value"}, [][]string{{"name", "Ada"}, {"mail", "ada@example.com"}, {"role", "admin"}}},
		{"queue:jobs", []string{"Index", "Value"}, [][]string{{"0", "build"}, {"1", "test"}, {"2", "deploy"}}},
		{"user:42:tags", []string{"Member"}, [][]string{{"go"}, {"redis"}, {"ui"}}},
		{"scores", []string{"Member", "Score"}, [][]string{{"carol", "7.5"}, {"alice", "9.25"}, {"bob", "12"}}},
		{"events", []string{"ID", "Time", "Fields"}, [][]string{
			{"1700000000001-0", "2023-11-14T22:13:20.001Z", "type=logout  user=ada"},
			{"1700000000000-0", "2023-11-14T22:13:20Z", "type=login  user=ada"}}},
	}
	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			c := newColl(t, fixtureServer(), tc.key)
			if !c.More() {
				t.Error("nothing is loaded yet, so there is more")
			}
			fetchAll(t, c)
			var cols []string
			for _, col := range c.Columns() {
				cols = append(cols, col.Name)
			}
			if !slices.Equal(cols, tc.cols) {
				t.Errorf("columns = %q", cols)
			}
			if got := rowsOf(c); !reflect.DeepEqual(got, tc.rows) {
				t.Errorf("rows = %q\nwant   %q", got, tc.rows)
			}
		})
	}
}

func TestNewCollectionRefusesATypeWithNoRows(t *testing.T) {
	for _, typ := range []string{TypeString, TypeJSON, TypeNone, "TSDB-TYPE"} {
		_, err := NewCollection(nil, "k", typ)
		wantRoute(t, err, "NewCollection.RedisTypeNotCollection")
	}
}

// bigFixture has a key of every collection type with n rows.
func bigFixture(n int) *fakeServer {
	s := newFakeServer()
	h := &fakeKey{typ: TypeHash}
	l := &fakeKey{typ: TypeList}
	st := &fakeKey{typ: TypeSet}
	z := &fakeKey{typ: TypeZSet}
	x := &fakeKey{typ: TypeStream}
	for i := 0; i < n; i++ {
		h.hash = append(h.hash, Pair{"f" + strconv.Itoa(i), "v" + strconv.Itoa(i)})
		l.list = append(l.list, "item"+strconv.Itoa(i))
		st.set = append(st.set, "m"+strconv.Itoa(i))
		z.zset = append(z.zset, ZMember{"z" + strconv.Itoa(i), float64(i)})
		x.stream = append(x.stream, StreamEntry{ID: fmt.Sprintf("%d-0", 1700000000000+i), Fields: []Pair{{"n", strconv.Itoa(i)}}})
	}
	s.put(0, "hash", h)
	s.put(0, "list", l)
	s.put(0, "set", st)
	s.put(0, "zset", z)
	s.put(0, "stream", x)
	return s
}

func TestPagesLoadOneAfterTheOther(t *testing.T) {
	const n = ValuePageSize*2 + 50
	for _, key := range []string{"hash", "list", "set", "zset", "stream"} {
		t.Run(key, func(t *testing.T) {
			c := newColl(t, bigFixture(n), key)
			var sizes []int
			for c.More() {
				got, err := c.Fetch(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				sizes = append(sizes, got)
				if len(sizes) > 10 {
					t.Fatal("too many pages")
				}
			}
			if c.Len() != n {
				t.Fatalf("rows = %d, want %d (pages %v)", c.Len(), n, sizes)
			}
			if len(sizes) != 3 {
				t.Errorf("pages = %v, want 3", sizes)
			}
			if got, err := c.Fetch(context.Background()); got != 0 || err != nil {
				t.Errorf("Fetch past the end = %d, %v", got, err)
			}
			// no row twice
			seen := map[string]bool{}
			for _, r := range rowsOf(c) {
				id := r[0]
				if key == "list" {
					id = r[0]
				}
				if seen[id] {
					t.Fatalf("row %q twice", id)
				}
				seen[id] = true
			}
		})
	}
}

func TestSCANPagesThatAreSmallOrEmptyStillLoadEverything(t *testing.T) {
	// the server returns two rows per call whatever COUNT says
	s := bigFixture(30)
	s.batch = 2
	for _, key := range []string{"hash", "set", "zset"} {
		c := newColl(t, s, key)
		fetchAll(t, c)
		if c.Len() != 30 {
			t.Errorf("%s: rows = %d, want 30", key, c.Len())
		}
	}
}

func TestStreamPagesGoBackwardsWithoutRepeating(t *testing.T) {
	c := newColl(t, bigFixture(ValuePageSize+3), "stream")
	fetchAll(t, c)
	rows := rowsOf(c)
	if rows[0][0] != fmt.Sprintf("%d-0", 1700000000000+ValuePageSize+2) || rows[len(rows)-1][0] != "1700000000000-0" {
		t.Errorf("first %q last %q", rows[0][0], rows[len(rows)-1][0])
	}
}

func TestIDBefore(t *testing.T) {
	tests := []struct {
		id, want string
		ok       bool
	}{
		{"1700000000000-5", "1700000000000-4", true},
		{"1700000000000-0", "1699999999999-18446744073709551615", true},
		{"5", "4-18446744073709551615", true},
		{"0-1", "0-0", true},
		{"0-0", "", false},
		{"abc", "", false},
		{"1-x", "", false},
	}
	for _, tc := range tests {
		got, ok := idBefore(tc.id)
		if got != tc.want || ok != tc.ok {
			t.Errorf("idBefore(%q) = %q, %v; want %q, %v", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}

func TestHashChanges(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "user:42:profile")
	fetchAll(t, c)
	if err := c.Add(ctx, []string{"city", "Athens"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, []string{"name", "Ada L."}); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"name", "Ada L."}, {"mail", "ada@example.com"}, {"role", "admin"}, {"city", "Athens"}}
	if got := rowsOf(c); !reflect.DeepEqual(got, want) {
		t.Errorf("after Add = %q", got)
	}
	form, err := c.EditForm(1)
	if err != nil || form[0].Value != "mail" || !form[0].Disabled || form[1].Value != "ada@example.com" {
		t.Fatalf("EditForm = %+v, %v", form, err)
	}
	if err := c.Edit(ctx, 1, []string{"mail", "ada@new.example"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, []int{0, 2}); err != nil {
		t.Fatal(err)
	}
	want = [][]string{{"mail", "ada@new.example"}, {"city", "Athens"}}
	if got := rowsOf(c); !reflect.DeepEqual(got, want) {
		t.Errorf("after the changes = %q", got)
	}
	// the server holds the same
	c.Reset()
	fetchAll(t, c)
	if got := rowsOf(c); !reflect.DeepEqual(got, want) {
		t.Errorf("on the server = %q", got)
	}
	if c.NeedsReload() || !c.CanEdit() || c.AddNoun() != "Field" || c.Type() != TypeHash {
		t.Error("hash facts")
	}
}

func TestHashErrors(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	s.put(0, "bin", &fakeKey{typ: TypeHash, hash: []Pair{{"blob", "\xff\x00"}}})
	c := newColl(t, s, "user:42:profile")
	fetchAll(t, c)
	wantRoute(t, c.Add(ctx, []string{"", "v"}), "hashColl.Add->need.RedisFieldMissing")
	wantRoute(t, c.Add(ctx, nil), "hashColl.Add->need.RedisFieldMissing")
	_, err := c.EditForm(9)
	wantRoute(t, err, "hashColl.EditForm.RedisRowNotFound")
	wantRoute(t, c.Edit(ctx, 9, []string{"x", "y"}), "hashColl.Edit.RedisRowNotFound")
	wantRoute(t, c.Remove(ctx, []int{9}), "hashColl.Remove.RedisRowNotFound")
	wantRoute(t, c.Remove(ctx, nil), "hashColl.Remove.RedisRowNotFound")
	b := newColl(t, s, "bin")
	fetchAll(t, b)
	_, err = b.EditForm(0)
	wantRoute(t, err, "hashColl.EditForm->requireText.RedisRowNotEditable")
	if got := rowsOf(b); got[0][1] != "0xff00" {
		t.Errorf("binary value shown as %q", got[0][1])
	}
	// the server's errors reach the caller and the rows stay as they were
	s.fail["HSET"] = errors.New("Boom", "boom")
	if err := c.Add(ctx, []string{"f", "v"}); err == nil || c.Len() != 3 {
		t.Errorf("Add with a failing server: %v, %d rows", err, c.Len())
	}
	s.fail["HSET"] = errors.New("Boom", "boom")
	if err := c.Edit(ctx, 0, []string{"name", "z"}); err == nil || rowsOf(c)[0][1] != "Ada" {
		t.Errorf("Edit with a failing server: %v, %q", err, rowsOf(c)[0])
	}
	s.fail["HDEL"] = errors.New("Boom", "boom")
	if err := c.Remove(ctx, []int{0}); err == nil || c.Len() != 3 {
		t.Errorf("Remove with a failing server: %v, %d rows", err, c.Len())
	}
	s.fail["HSCAN"] = errors.New("Boom", "boom")
	c.Reset()
	if _, err := c.Fetch(ctx); err == nil {
		t.Error("Fetch lost the error of the server")
	}
}

func TestListChanges(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "queue:jobs")
	fetchAll(t, c)
	if err := c.Edit(ctx, 1, []string{"TEST"}); err != nil {
		t.Fatal(err)
	}
	if got := rowsOf(c)[1][1]; got != "TEST" {
		t.Errorf("edited row = %q", got)
	}
	if err := c.Add(ctx, []string{"head item", "head"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, []string{"tail item"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, []int{1, 3}); err != nil { // after the push at the head: index 1 is "build", 3 is "deploy"
		// the collection was not loaded again, so its indexes are the old ones: that is what NeedsReload is for
		t.Fatal(err)
	}
	if !c.NeedsReload() {
		t.Fatal("a list must be loaded again after Add and Remove")
	}
	c.Reset()
	fetchAll(t, c)
	items := []string{}
	for _, r := range rowsOf(c) {
		items = append(items, r[1])
	}
	// the list on the server: head item, build, TEST, deploy, tail item; removing indexes 3 then 1 leaves head item, TEST, tail item
	if want := []string{"head item", "TEST", "tail item"}; !slices.Equal(items, want) {
		t.Errorf("list = %q, want %q", items, want)
	}
}

func TestListErrors(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "queue:jobs")
	fetchAll(t, c)
	_, err := c.EditForm(7)
	wantRoute(t, err, "listColl.EditForm.RedisRowNotFound")
	wantRoute(t, c.Edit(ctx, 7, []string{"x"}), "listColl.Edit.RedisRowNotFound")
	wantRoute(t, c.Remove(ctx, nil), "listColl.Remove.RedisRowNotFound")
	s.put(0, "bin", &fakeKey{typ: TypeList, list: []string{"\xff"}})
	b := newColl(t, s, "bin")
	fetchAll(t, b)
	_, err = b.EditForm(0)
	wantRoute(t, err, "listColl.EditForm->requireText.RedisRowNotEditable")
	form, err := c.EditForm(2)
	if err != nil || form[0].Value != "deploy" || form[0].Label != "Value of item 2" {
		t.Errorf("EditForm = %+v, %v", form, err)
	}
	s.fail["LSET"] = errors.New("Boom", "boom")
	if err := c.Edit(ctx, 0, []string{"z"}); err == nil || rowsOf(c)[0][1] != "build" {
		t.Errorf("Edit with a failing server: %v", err)
	}
	s.fail["LRANGE"] = errors.New("Boom", "boom")
	c.Reset()
	if _, err := c.Fetch(ctx); err == nil {
		t.Error("Fetch lost the error of the server")
	}
	s.fail["LREMOVEAT"] = errors.New("Boom", "boom")
	if err := c.Remove(ctx, []int{0}); err == nil {
		t.Error("Remove lost the error of the server")
	}
	s.fail["RPUSH"] = errors.New("Boom", "boom")
	if err := c.Add(ctx, []string{"x"}); err == nil {
		t.Error("Add lost the error of the server")
	}
}

func TestSetChanges(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "user:42:tags")
	fetchAll(t, c)
	if err := c.Add(ctx, []string{"tui"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, []string{"go"}); err != nil { // there already
		t.Fatal(err)
	}
	if err := c.Remove(ctx, []int{0, 2}); err != nil {
		t.Fatal(err)
	}
	if got, want := rowsOf(c), [][]string{{"redis"}, {"tui"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
	if c.CanEdit() || c.NeedsReload() || c.AddNoun() != "Member" {
		t.Error("set facts")
	}
	_, err := c.EditForm(0)
	wantRoute(t, err, "setColl.EditForm.RedisRowNotEditable")
	wantRoute(t, c.Edit(ctx, 0, nil), "setColl.Edit.RedisRowNotEditable")
	wantRoute(t, c.Edit(ctx, 9, nil), "setColl.Edit.RedisRowNotFound")
	wantRoute(t, c.Add(ctx, []string{" "}), "setColl.Add->need.RedisFieldMissing")
	wantRoute(t, c.Remove(ctx, []int{9}), "setColl.Remove.RedisRowNotFound")
	s.fail["SADD"] = errors.New("Boom", "boom")
	if err := c.Add(ctx, []string{"z"}); err == nil {
		t.Error("Add lost the error of the server")
	}
	s.fail["SREM"] = errors.New("Boom", "boom")
	if err := c.Remove(ctx, []int{0}); err == nil || c.Len() != 2 {
		t.Errorf("Remove with a failing server: %v, %d rows", err, c.Len())
	}
	s.fail["SSCAN"] = errors.New("Boom", "boom")
	c.Reset()
	if _, err := c.Fetch(ctx); err == nil {
		t.Error("Fetch lost the error of the server")
	}
}

func TestSortedSetChanges(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "scores")
	fetchAll(t, c)
	if err := c.Add(ctx, []string{"dave", "inf"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(ctx, []string{"carol", "8"}); err != nil {
		t.Fatal(err)
	}
	form, err := c.EditForm(1)
	if err != nil || form[0].Value != "alice" || !form[0].Disabled || form[1].Value != "9.25" {
		t.Fatalf("EditForm = %+v, %v", form, err)
	}
	if err := c.Edit(ctx, 1, []string{"alice", "-inf"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, []int{2}); err != nil {
		t.Fatal(err)
	}
	if got, want := rowsOf(c), [][]string{{"carol", "8"}, {"alice", "-Inf"}, {"dave", "+Inf"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
	if !c.CanEdit() || c.NeedsReload() || c.Type() != TypeZSet {
		t.Error("sorted set facts")
	}
}

func TestSortedSetErrors(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "scores")
	fetchAll(t, c)
	wantRoute(t, c.Add(ctx, []string{"", "1"}), "zsetColl.Add->need.RedisFieldMissing")
	wantRoute(t, c.Add(ctx, []string{"m", ""}), "zsetColl.Add->need.RedisFieldMissing")
	wantRoute(t, c.Add(ctx, []string{"m", "abc"}), "zsetColl.Add->ParseScore.RedisScoreInvalid")
	wantRoute(t, c.Add(ctx, []string{"m", "nan"}), "zsetColl.Add->ParseScore.RedisScoreInvalid")
	wantRoute(t, c.Edit(ctx, 0, []string{"carol", "x"}), "zsetColl.Edit->ParseScore.RedisScoreInvalid")
	wantRoute(t, c.Edit(ctx, 0, []string{"carol", ""}), "zsetColl.Edit->need.RedisFieldMissing")
	wantRoute(t, c.Edit(ctx, 9, []string{"carol", "1"}), "zsetColl.Edit.RedisRowNotFound")
	_, err := c.EditForm(9)
	wantRoute(t, err, "zsetColl.EditForm.RedisRowNotFound")
	wantRoute(t, c.Remove(ctx, nil), "zsetColl.Remove.RedisRowNotFound")
	s.put(0, "bin", &fakeKey{typ: TypeZSet, zset: []ZMember{{"\xff", 1}}})
	b := newColl(t, s, "bin")
	fetchAll(t, b)
	_, err = b.EditForm(0)
	wantRoute(t, err, "zsetColl.EditForm->requireText.RedisRowNotEditable")
	for _, op := range []string{"ZADD", "ZREM", "ZSCAN"} {
		s.fail[op] = errors.New("Boom", "boom")
		var err error
		switch op {
		case "ZADD":
			err = c.Add(ctx, []string{"q", "1"})
		case "ZREM":
			err = c.Remove(ctx, []int{0})
		case "ZSCAN":
			c.Reset()
			_, err = c.Fetch(ctx)
		}
		if err == nil {
			t.Errorf("%s: the error of the server was lost", op)
		}
	}
}

func TestScoreText(t *testing.T) {
	for _, tc := range []struct {
		f    float64
		want string
	}{{1, "1"}, {9.25, "9.25"}, {1e21, "1e+21"}, {-0.5, "-0.5"}} {
		if got := FormatScore(tc.f); got != tc.want {
			t.Errorf("FormatScore(%v) = %q", tc.f, got)
		}
		back, err := ParseScore(FormatScore(tc.f))
		if err != nil || back != tc.f {
			t.Errorf("ParseScore(FormatScore(%v)) = %v, %v", tc.f, back, err)
		}
	}
	for _, in := range []string{"inf", "+inf", "INF", "-inf", " 3 "} {
		if _, err := ParseScore(in); err != nil {
			t.Errorf("ParseScore(%q): %v", in, err)
		}
	}
}

func TestStreamChanges(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "events")
	fetchAll(t, c)
	if err := c.Add(ctx, []string{"1700000000002-0", "type tick\nn   5"}); err != nil {
		t.Fatal(err)
	}
	if !c.NeedsReload() || c.CanEdit() || c.AddNoun() != "Entry" {
		t.Error("stream facts")
	}
	c.Reset()
	fetchAll(t, c)
	if rows := rowsOf(c); len(rows) != 3 || rows[0][0] != "1700000000002-0" || rows[0][2] != "type=tick  n=  5" {
		t.Errorf("rows = %q", rows)
	}
	if err := c.Remove(ctx, []int{0, 2}); err != nil {
		t.Fatal(err)
	}
	c.Reset()
	fetchAll(t, c)
	if rows := rowsOf(c); len(rows) != 1 || rows[0][0] != "1700000000001-0" {
		t.Errorf("rows after the removal = %q", rows)
	}
	// the id is the server's when left empty
	if err := c.Add(ctx, []string{"", "a b"}); err != nil {
		t.Fatal(err)
	}
}

func TestStreamErrors(t *testing.T) {
	ctx := context.Background()
	s := fixtureServer()
	c := newColl(t, s, "events")
	fetchAll(t, c)
	_, err := c.EditForm(0)
	wantRoute(t, err, "streamColl.EditForm.RedisRowNotEditable")
	wantRoute(t, c.Edit(ctx, 0, nil), "streamColl.Edit.RedisRowNotEditable")
	wantRoute(t, c.Edit(ctx, 9, nil), "streamColl.Edit.RedisRowNotFound")
	wantRoute(t, c.Add(ctx, []string{"", ""}), "streamColl.Add->need.RedisFieldMissing")
	wantRoute(t, c.Remove(ctx, []int{9}), "streamColl.Remove.RedisRowNotFound")
	wantRoute(t, func() error { _, err := ParseEntryFields("\n  \n"); return err }(), "ParseEntryFields.RedisStreamFieldsInvalid")
	// an id that is not after the last one is the server's error
	if err := c.Add(ctx, []string{"1-0", "a b"}); err == nil {
		t.Error("the error of the server was lost")
	}
	s.fail["XDEL"] = errors.New("Boom", "boom")
	if err := c.Remove(ctx, []int{0}); err == nil {
		t.Error("Remove lost the error of the server")
	}
	s.fail["XREVRANGE"] = errors.New("Boom", "boom")
	c.Reset()
	if _, err := c.Fetch(ctx); err == nil {
		t.Error("Fetch lost the error of the server")
	}
}

func TestParseEntryFields(t *testing.T) {
	got, err := ParseEntryFields("temperature 21.5\r\n  humidity   40 %\n\nflag\n")
	want := []Pair{{"temperature", "21.5"}, {"humidity", "  40 %"}, {"flag", ""}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ParseEntryFields = %+v, %v", got, err)
	}
}

func TestFormatEntryFields(t *testing.T) {
	if got := FormatEntryFields([]Pair{{"a", "1"}, {"a", "2"}, {"b", ""}}); got != "a=1  a=2  b=" {
		t.Errorf("FormatEntryFields = %q", got)
	}
}

func TestRowFormsKeepValuesAndChoices(t *testing.T) {
	c := newColl(t, fixtureServer(), "queue:jobs")
	f := newRowForm("Add item", c.AddForm())
	defer f.dispose()
	if f.missing() != "" {
		t.Errorf("missing = %q", f.missing())
	}
	f.ctl[0].SetValue("hello")
	f.choice[1] = 1
	if got := f.values(); !slices.Equal(got, []string{"hello", "head"}) {
		t.Errorf("values = %q", got)
	}
	h := newRowForm("Add field", newColl(t, fixtureServer(), "user:42:profile").AddForm())
	defer h.dispose()
	if h.missing() != "Field" {
		t.Errorf("missing = %q, want Field", h.missing())
	}
}
