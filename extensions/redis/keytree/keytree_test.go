package keytree

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// names lists the rows of a path as "name/" for a folder (with its count) and "name" for a key.
func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		if e.Folder {
			out[i] = fmt.Sprintf("%s/(%d)", e.Name, e.Count)
		} else {
			out[i] = e.Name
		}
	}
	return out
}

func keys(ks ...string) []Leaf {
	out := make([]Leaf, len(ks))
	for i, k := range ks {
		out[i] = Leaf{Key: k, Type: "string", TTLMillis: -1}
	}
	return out
}

func TestChildrenSplitAtTheSeparator(t *testing.T) {
	tests := []struct {
		name string
		sep  string
		keys []string
		path string
		want []string
	}{
		{"top of a simple tree", ":", []string{"user:42:name", "user:42:mail", "user:7:name", "session:abc", "plain"}, "",
			[]string{"session/(1)", "user/(3)", "plain"}},
		{"one level down", ":", []string{"user:42:name", "user:42:mail", "user:7:name"}, "user:",
			[]string{"7/(1)", "42/(2)"}},
		{"two levels down", ":", []string{"user:42:name", "user:42:mail", "user:7:name"}, "user:42:",
			[]string{"mail", "name"}},
		{"a key that is also a folder", ":", []string{"user", "user:1"}, "",
			[]string{"user/(1)", "user"}},
		{"empty segments", ":", []string{"a::b", "a:c"}, "a:",
			[]string{"/(1)", "c"}},
		{"inside an empty segment", ":", []string{"a::b", "a:c"}, "a::",
			[]string{"b"}},
		{"a key that starts with the separator", ":", []string{":x", "y"}, "",
			[]string{"/(1)", "y"}},
		{"a key that ends with the separator", ":", []string{"a:"}, "a:",
			[]string{""}},
		{"a longer separator", "::", []string{"a::b", "a:b"}, "",
			[]string{"a/(1)", "a:b"}},
		{"a slash", "/", []string{"img/a.png", "img/b.png"}, "img/",
			[]string{"a.png", "b.png"}},
		{"no separator makes a flat tree", "", []string{"a:b", "c"}, "",
			[]string{"a:b", "c"}},
		{"natural order", ":", []string{"k:10", "k:2", "k:1", "k:a"}, "k:",
			[]string{"1", "2", "10", "a"}},
		{"a path that is not there", ":", []string{"a:b"}, "zzz:", nil},
		{"a key is not a folder", ":", []string{"a:b"}, "a:b:", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := New(tc.sep)
			tr.Add(keys(tc.keys...)...)
			if got := names(tr.Children(tc.path)); !slices.Equal(got, tc.want) {
				t.Errorf("Children(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestAddCountsNewKeysAndReplacesOldOnes(t *testing.T) {
	tr := New(":")
	if n := tr.Add(keys("a:b", "a:c")...); n != 2 {
		t.Errorf("added %d, want 2", n)
	}
	leaf := Leaf{Key: "a:b", Type: "hash", TTLMillis: 5000}
	if n := tr.Add(leaf, Leaf{Key: "a:d"}); n != 1 {
		t.Errorf("added %d, want 1 (a:b was there)", n)
	}
	if tr.Len() != 3 || tr.CountBelow("a:") != 3 {
		t.Errorf("Len %d, below a %d, want 3 and 3", tr.Len(), tr.CountBelow("a:"))
	}
	got, ok := tr.Get("a:b")
	if !ok || got != leaf {
		t.Errorf("Get = %+v, %v", got, ok)
	}
	if _, ok := tr.Get("a:zz"); ok {
		t.Error("a key that was never added is there")
	}
	if _, ok := tr.Get("q:b"); ok {
		t.Error("a key below a folder that is not there")
	}
}

func TestRemoveDropsEmptyFolders(t *testing.T) {
	tr := New(":")
	tr.Add(keys("a:b:c", "a:d", "e")...)
	if !tr.Remove("a:b:c") {
		t.Fatal("a:b:c was not found")
	}
	if got, want := names(tr.Children("a:")), []string{"d"}; !slices.Equal(got, want) {
		t.Errorf("below a = %q, want %q (b is empty and goes)", got, want)
	}
	if tr.Remove("a:b:c") || tr.Remove("nope") || tr.Remove("x:y") {
		t.Error("removed a key that is not there")
	}
	tr.Remove("a:d")
	if got, want := names(tr.Children("")), []string{"e"}; !slices.Equal(got, want) {
		t.Errorf("top = %q, want %q", got, want)
	}
	if tr.Len() != 1 || tr.CountBelow("a:") != 0 {
		t.Errorf("Len %d, below a %d", tr.Len(), tr.CountBelow("a:"))
	}
}

func TestEntriesCarryPathsThatChildrenTakesBack(t *testing.T) {
	tr := New(":")
	tr.Add(keys("a:b:c", "a:b:d", "a:e")...)
	top := tr.Children("")
	if len(top) != 1 || !top[0].Folder || top[0].Path != "a:" || top[0].Count != 3 {
		t.Fatalf("top = %+v", top)
	}
	mid := tr.Children(top[0].Path)
	if got, want := names(mid), []string{"b/(2)", "e"}; !slices.Equal(got, want) {
		t.Fatalf("mid = %q", got)
	}
	if mid[0].Path != "a:b:" || mid[1].Path != "a:e" || mid[1].Leaf.Key != "a:e" {
		t.Errorf("paths = %q, %q", mid[0].Path, mid[1].Path)
	}
	if got := names(tr.Children(mid[0].Path)); !slices.Equal(got, []string{"c", "d"}) {
		t.Errorf("below a:b: = %q", got)
	}
}

func TestKeysListsEverything(t *testing.T) {
	tr := New(":")
	in := []string{"a:b:c", "a:d", "e", "f:"}
	tr.Add(keys(in...)...)
	got := tr.Keys()
	slices.Sort(got)
	slices.Sort(in)
	if !slices.Equal(got, in) {
		t.Errorf("Keys = %q, want %q", got, in)
	}
}

func TestNaturalCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"a", "a", 0}, {"a", "b", -1}, {"b", "a", 1}, {"a1", "a2", -1}, {"a2", "a10", -1}, {"a10", "a2", 1},
		{"a007", "a7", -1}, {"a7", "a007", 1}, {"", "a", -1}, {"a", "", 1}, {"a1b", "a1c", -1}, {"1", "a", -1},
		{"x99999999999999999999", "x100000000000000000000", -1},
	}
	for _, tc := range tests {
		t.Run(tc.a+"|"+tc.b, func(t *testing.T) {
			if got := NaturalCompare(tc.a, tc.b); got != tc.want {
				t.Errorf("NaturalCompare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestSeededTreesAgreeWithAFlatList builds trees from random keys and checks every level against a plain walk of the key list.
func TestSeededTreesAgreeWithAFlatList(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		alphabet := []string{"a", "b", "c", "", "10", "2"}
		seen := map[string]bool{}
		var all []string
		for i := 0; i < 200; i++ {
			parts := make([]string, 1+rng.Intn(4))
			for j := range parts {
				parts[j] = alphabet[rng.Intn(len(alphabet))]
			}
			k := strings.Join(parts, ":")
			if !seen[k] {
				seen[k] = true
				all = append(all, k)
			}
		}
		tr := New(":")
		tr.Add(keys(all...)...)
		if tr.Len() != len(all) {
			t.Fatalf("seed %d: Len %d, want %d", seed, tr.Len(), len(all))
		}
		// every key must be reachable by walking Children from the top
		reached := 0
		var walk func(path string)
		walk = func(path string) {
			for _, e := range tr.Children(path) {
				if e.Folder {
					below := 0
					for _, k := range all {
						if strings.HasPrefix(k, e.Path) {
							below++
						}
					}
					if below != e.Count {
						t.Fatalf("seed %d: folder %q counts %d, flat list says %d", seed, e.Path, e.Count, below)
					}
					walk(e.Path)
				} else {
					if !seen[e.Leaf.Key] {
						t.Fatalf("seed %d: invented key %q", seed, e.Leaf.Key)
					}
					reached++
				}
			}
		}
		walk("")
		if reached != len(all) {
			t.Fatalf("seed %d: reached %d keys of %d", seed, reached, len(all))
		}
		// remove half and check again
		for i, k := range all {
			if i%2 == 0 && !tr.Remove(k) {
				t.Fatalf("seed %d: cannot remove %q", seed, k)
			}
		}
		if tr.Len() != len(all)/2 {
			t.Fatalf("seed %d: after removing, Len %d, want %d", seed, tr.Len(), len(all)/2)
		}
	}
}

// TestVeryManyKeysAreBuiltLazily adds a large keyspace in pages, as SCAN does, from a generator that never holds the list, and
// checks that the top level stays small and cheap to list.
func TestVeryManyKeysAreBuiltLazily(t *testing.T) {
	const total = 200_000
	if testing.Short() {
		t.Skip("a large keyspace")
	}
	tr := New(":")
	page := make([]Leaf, 0, 500)
	for i := 0; i < total; i++ {
		page = append(page, Leaf{Key: fmt.Sprintf("tenant:%d:item:%d", i%50, i), Type: "string", TTLMillis: -1})
		if len(page) == cap(page) {
			tr.Add(page...)
			page = page[:0]
		}
	}
	tr.Add(page...)
	if tr.Len() != total {
		t.Fatalf("Len = %d, want %d", tr.Len(), total)
	}
	top := tr.Children("")
	if len(top) != 1 || top[0].Count != total {
		t.Fatalf("top = %d rows, first count %d", len(top), top[0].Count)
	}
	tenants := tr.Children("tenant:")
	if len(tenants) != 50 || tenants[0].Name != "0" || tenants[49].Name != "49" {
		t.Errorf("tenants = %d rows, %q..%q", len(tenants), tenants[0].Name, tenants[len(tenants)-1].Name)
	}
	if n := len(tr.Children("tenant:7:item:")); n != total/50 {
		t.Errorf("items of a tenant = %d, want %d", n, total/50)
	}
}
