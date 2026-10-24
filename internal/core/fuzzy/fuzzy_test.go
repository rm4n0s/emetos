package fuzzy

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests of the conventions of the Go package (positions in runes, scratch buffers, bounds).

func TestRuneOffsets(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		query string
		label []Match
		desc  []Match
	}{
		{"accents in the label", "src/Ünïcödé.go", "ncö", []Match{{1, 2}, {3, 5}}, nil},
		{"prefix of an accented label", "src/ünïcödé.go", "ünï", []Match{{0, 3}}, nil},
		{"astral rune before the match", "src/\U0001F680rocket.go", "rocket", []Match{{1, 7}}, nil},
		{"CJK in the description", "项目/文件/main.go", "文main", []Match{{0, 4}}, []Match{{3, 4}}},
		{"multi-byte description and a label match", "ünï/cödé/file.go", "cfile", []Match{{0, 4}}, []Match{{4, 5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ScoreItemFuzzy[string](nil, tt.path, PrepareQuery(tt.query), true, pathAccessor{}, nil)
			if res.Score == 0 {
				t.Fatal("no score")
			}
			if !slices.Equal(res.LabelMatch, tt.label) {
				t.Errorf("label match = %v, want %v", res.LabelMatch, tt.label)
			}
			if tt.desc != nil && !slices.Equal(res.DescriptionMatch, tt.desc) {
				t.Errorf("description match = %v, want %v", res.DescriptionMatch, tt.desc)
			}
			// the matches are runes of the strings
			label := []rune(pathAccessor{}.ItemLabel(tt.path))
			for _, m := range res.LabelMatch {
				if m.Start < 0 || m.End > len(label) || m.Start >= m.End {
					t.Errorf("label match %v out of the %d runes of %q", m, len(label), string(label))
				}
			}
			desc := []rune(pathAccessor{}.ItemDescription(tt.path))
			for _, m := range res.DescriptionMatch {
				if m.Start < 0 || m.End > len(desc) {
					t.Errorf("description match %v out of the %d runes of %q", m, len(desc), string(desc))
				}
			}
		})
	}

	t.Run("identity counts runes", func(t *testing.T) {
		path := "ünï/cödé/file.go"
		res := ScoreItemFuzzy[string](nil, path, PrepareQuery(path), true, pathAccessor{}, nil)
		if res.Score != PathIdentityScore ||
			!slices.Equal(res.LabelMatch, []Match{{0, 7}}) || !slices.Equal(res.DescriptionMatch, []Match{{0, 8}}) {
			t.Errorf("identity = %+v", res)
		}
	})

	t.Run("ScoreFuzzy positions", func(t *testing.T) {
		q := PrepareQuery("ö")
		got := ScoreFuzzy("ünï-cödé", q.Normalized, q.NormalizedLowercase, true)
		if !slices.Equal(got.Positions, []int{5}) {
			t.Errorf("positions = %v", got.Positions)
		}
		// the case of a non-ASCII letter counts: the upper case Ö is found by ö
		got = ScoreFuzzy("ÜNÏ-CÖDÉ", q.Normalized, q.NormalizedLowercase, true)
		if !slices.Equal(got.Positions, []int{5}) {
			t.Errorf("positions in upper case = %v", got.Positions)
		}
	})

	t.Run("ScoreFuzzy2 matches", func(t *testing.T) {
		res := ScoreFuzzy2("\U0001F680 launch", PrepareQuery("la"), 0, 0)
		if !res.Matched || !slices.Equal(res.Matches, []Match{{2, 4}}) {
			t.Errorf("matches = %+v", res)
		}
	})
}

// randomString makes a string of the alphabet, which has ASCII, separators, upper case and
// non-ASCII characters.
func randomString(rng *rand.Rand, alphabet []rune, minLen, maxLen int) string {
	n := minLen + rng.IntN(maxLen-minLen+1)
	r := make([]rune, n)
	for i := range r {
		r[i] = alphabet[rng.IntN(len(alphabet))]
	}
	return string(r)
}

var (
	asciiAlphabet = []rune("abcdeABCDE/._-  fgFG12")
	mixedAlphabet = []rune("abcAB/._-éÉü\U0001F680界xyXY")
)

func TestScratchReuseGivesTheSameResults(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	reused := NewScorer()
	for _, alphabet := range [][]rune{asciiAlphabet, mixedAlphabet} {
		for i := 0; i < 1500; i++ {
			// the sizes vary so that the scratch buffers are used at different fills
			target := randomString(rng, alphabet, 1, 40+i%60)
			query := randomString(rng, alphabet, 1, 8)
			prepared := PrepareQuery(query)
			desc := randomString(rng, alphabet, 0, 30)
			path := desc + "/" + target
			item := path

			for _, allow := range []bool{true, false} {
				if got, want := reused.ScoreFuzzy(target, prepared.Normalized, prepared.NormalizedLowercase, allow),
					NewScorer().ScoreFuzzy(target, prepared.Normalized, prepared.NormalizedLowercase, allow); !reflect.DeepEqual(got, want) {
					t.Fatalf("ScoreFuzzy(%q, %q, %v) reused = %+v, fresh = %+v", target, query, allow, got, want)
				}
				if got, want := ScoreItemFuzzy[string](reused, item, prepared, allow, pathAccessor{}, nil),
					ScoreItemFuzzy[string](NewScorer(), item, prepared, allow, pathAccessor{}, nil); !reflect.DeepEqual(got, want) {
					t.Fatalf("ScoreItemFuzzy(%q, %q, %v) reused = %+v, fresh = %+v", item, query, allow, got, want)
				}
			}
			ql := strings.ToLower(query)
			for _, opts := range []FuzzyScoreOptions{DefaultFuzzyScoreOptions, {FirstMatchCanBeWeak: true, BoostFullMatch: true}} {
				got, gotOK := reused.FuzzyScore(query, ql, 0, target, strings.ToLower(target), 0, opts)
				want, wantOK := NewScorer().FuzzyScore(query, ql, 0, target, strings.ToLower(target), 0, opts)
				if gotOK != wantOK || !reflect.DeepEqual(got, want) {
					t.Fatalf("FuzzyScore(%q, %q) reused = %+v, fresh = %+v", query, target, got, want)
				}
			}
			if got, want := reused.ScoreFuzzy2(target, prepared, 0, 0), NewScorer().ScoreFuzzy2(target, prepared, 0, 0); !reflect.DeepEqual(got, want) {
				t.Fatalf("ScoreFuzzy2(%q, %q) reused = %+v, fresh = %+v", target, query, got, want)
			}
		}
	}
}

// TestQuickRejectKeepsResults checks the two shortcuts that skip the matrix: the check on bytes
// and the check on runes. A query that is not a subsequence has no score, and the score of a
// query that is one is positive.
func TestQuickRejectKeepsResults(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	s := NewScorer()
	acc := pathAccessor{}
	checked := 0
	for _, alphabet := range [][]rune{asciiAlphabet, mixedAlphabet} {
		for i := 0; i < 4000; i++ {
			item := randomString(rng, alphabet, 1, 12) + "/" + randomString(rng, alphabet, 1, 12)
			label, desc := acc.ItemLabel(item), acc.ItemDescription(item)
			q := PrepareQuery(randomString(rng, alphabet, 1, 4))
			ql := []rune(q.NormalizedLowercase)
			if len(ql) == 0 || len(q.Values) > 1 {
				continue
			}
			checked++

			lower := func(s string) []rune { return appendLower(nil, []rune(s)) }
			full := lower(desc + "/" + label)
			if sub, may := isSubsequence(full, ql), mayContain(ql, desc, label, true); sub && !may {
				t.Fatalf("mayContain rejects %q in %q, which is a subsequence", q.Normalized, item)
			}
			// a score needs the query to be a subsequence of the path (of the label when the
			// item has no description), and any such query scores
			want := isSubsequence(full, ql)
			if desc == "" {
				want = isSubsequence(lower(label), ql)
			}
			if label == "" {
				want = false // an item needs a label
			}
			if res := ScoreItemFuzzy[string](s, item, q, true, acc, nil); (res.Score > 0) != want {
				t.Fatalf("%q in %q: score %d, subsequence %v", q.Normalized, item, res.Score, want)
			}
		}
	}
	if checked < 2000 {
		t.Errorf("only %d cases were checked", checked)
	}
}

func TestLongInputsAreBounded(t *testing.T) {
	long := strings.Repeat("abcdefghij", 1000) // 10,000 runes
	start := time.Now()

	t.Run("long label, short query", func(t *testing.T) {
		s := NewScorer()
		q := PrepareQuery("aceg")
		res := s.ScoreFuzzy(long, q.Normalized, q.NormalizedLowercase, true)
		if res.Score == 0 || len(res.Positions) != 4 {
			t.Errorf("no match in a long label: %+v", res)
		}
		if cap(s.scores) > maxKeptCells {
			t.Errorf("the scorer holds %d cells of a big matrix", cap(s.scores))
		}
		q = PrepareQuery("abcdefghij")
		s.ScoreFuzzy(long, q.Normalized, q.NormalizedLowercase, true)
		if s.scores != nil {
			t.Errorf("the scorer holds %d cells of a matrix over the limit it keeps", cap(s.scores))
		}
	})

	t.Run("long query and label are cut off", func(t *testing.T) {
		s := NewScorer()
		q := PrepareQuery(long)
		// 10,000 by 10,000 is far over the bound of the matrix: no score, and no allocation of it
		res := s.ScoreFuzzy(long, q.Normalized, q.NormalizedLowercase, true)
		if res.Score != 0 {
			t.Errorf("score = %d, want none over the bound", res.Score)
		}
		if cap(s.scores) != 0 {
			t.Errorf("the scorer holds %d cells", cap(s.scores))
		}
	})

	t.Run("word scorer looks at 128 runes", func(t *testing.T) {
		r, ok := NewScorer().FuzzyScore("jjj", "jjj", 0, long, long, 0, FuzzyScoreOptions{FirstMatchCanBeWeak: true})
		if !ok {
			t.Fatal("no match in the first 128 runes")
		}
		for _, p := range r.Positions {
			if p >= 128 {
				t.Errorf("position %d is past the limit", p)
			}
		}
		// the first 'j' is the tenth rune; a pattern that needs the 129th rune finds nothing
		if _, ok := NewScorer().FuzzyScore(strings.Repeat("j", 14), strings.Repeat("j", 14), 0, long, long, 0, FuzzyScoreOptions{FirstMatchCanBeWeak: true}); ok {
			t.Error("a match past the limit of 128 runes")
		}
	})

	t.Run("item with a long label and description", func(t *testing.T) {
		item := long + "/" + long
		res := ScoreItemFuzzy[string](nil, item, PrepareQuery("jjjj"), true, pathAccessor{}, nil)
		if res.Score == 0 {
			t.Error("no match")
		}
		res = ScoreItemFuzzy[string](nil, item, PrepareQuery(long[:2000]), true, pathAccessor{}, nil)
		_ = res // over the bound: whatever the score, it returns
	})

	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("long inputs took %v", d)
	}
}

func TestSeparateScorersRunConcurrently(t *testing.T) {
	paths := benchPaths(2000)
	query := PrepareQuery("view model")
	want := make([]ItemScore, len(paths))
	s := NewScorer()
	for i, p := range paths {
		want[i] = ScoreItemFuzzy[string](s, p, query, true, pathAccessor{}, nil)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// the prepared query is shared: scoring only reads it
			s := NewScorer()
			cache := NewCache()
			for round := 0; round < 2; round++ {
				for i, p := range paths {
					if got := ScoreItemFuzzy[string](s, p, query, true, pathAccessor{}, cache); !reflect.DeepEqual(got, want[i]) {
						t.Errorf("path %q: %+v, want %+v", p, got, want[i])
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestPieceWithoutPrepareQuery(t *testing.T) {
	// a piece that was not made by PrepareQuery has no runes cached and still scores
	hand := &PreparedQuery{PreparedQueryPiece: PreparedQueryPiece{Original: "som", Normalized: "som", NormalizedLowercase: "som", PathNormalized: "som"}}
	made := PrepareQuery("som")
	path := "/xyz/some/path/someFile123.txt"
	got := ScoreItemFuzzy[string](nil, path, hand, true, pathAccessor{}, nil)
	want := ScoreItemFuzzy[string](nil, path, made, true, pathAccessor{}, nil)
	if got.Score == 0 || !reflect.DeepEqual(got, want) {
		t.Errorf("hand made = %+v, PrepareQuery = %+v", got, want)
	}
}

func TestNilScorerAndCache(t *testing.T) {
	q := PrepareQuery("fA")
	acc := pathAccessor{}
	a, b := "some/path/fileA.txt", "some/path/other/fileB.txt"
	if res := ScoreItemFuzzy[string](nil, a, q, true, acc, nil); res.Score == 0 {
		t.Error("no score with a nil scorer and a nil cache")
	}
	if CompareItemsByFuzzyScore[string](nil, a, b, q, true, acc, nil) >= 0 {
		t.Error("fileA should go before fileB for fA")
	}
}

func TestCompareIsAntisymmetric(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	paths := benchPaths(300)
	for range 10 {
		paths = append(paths, randomString(rng, mixedAlphabet, 1, 20)+"/"+randomString(rng, mixedAlphabet, 1, 10))
	}
	sign := func(n int) int {
		switch {
		case n < 0:
			return -1
		case n > 0:
			return 1
		}
		return 0
	}
	s := NewScorer()
	for _, query := range []string{"e", "view", "gui/ex", "mod ex", "b", "é"} {
		q := PrepareQuery(query)
		cache := NewCache()
		for i := 0; i < len(paths); i++ {
			for j := i; j < len(paths); j += 7 {
				ab := CompareItemsByFuzzyScore[string](s, paths[i], paths[j], q, true, pathAccessor{}, cache)
				ba := CompareItemsByFuzzyScore[string](s, paths[j], paths[i], q, true, pathAccessor{}, cache)
				if sign(ab) != -sign(ba) {
					t.Fatalf("query %q: compare(%q, %q) = %d but the reverse is %d", query, paths[i], paths[j], ab, ba)
				}
			}
		}
	}
}

func TestPrepareQueryDetails(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		normalized string
		values     int
		contiguous bool
		pathSep    bool
	}{
		{"no-break space and BOM are white space", "a\u00a0b\ufeffc", "abc", 0, false, false},
		{"tab and newline", "a\tb\nc", "abc", 0, false, false},
		{"hash is a piece of its own", "main #", "main", 2, false, false},
		{"two hashes", "main##", "main#", 0, false, false},
		{"quoted pieces are split at the space", `"a b" c`, "abc", 3, false, false},
		{"one quoted piece", `"abc"`, "abc", 0, true, false},
		{"only a quote", `"`, "", 0, true, false},
		{"empty", "", "", 0, false, false},
		{"path with backslashes", `a\b\c`, "a/b/c", 0, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := PrepareQuery(tt.query)
			if q.Normalized != tt.normalized {
				t.Errorf("Normalized = %q, want %q", q.Normalized, tt.normalized)
			}
			if len(q.Values) != tt.values {
				t.Errorf("%d values, want %d", len(q.Values), tt.values)
			}
			if q.ExpectContiguousMatch != tt.contiguous || q.ContainsPathSeparator != tt.pathSep {
				t.Errorf("contiguous = %v, path separator = %v", q.ExpectContiguousMatch, q.ContainsPathSeparator)
			}
		})
	}
}

func TestMatchesPrefixAndCreateMatches(t *testing.T) {
	tests := []struct {
		word, target string
		want         []Match
		ok           bool
	}{
		{"som", "someFile", []Match{{0, 3}}, true},
		{"SOM", "someFile", []Match{{0, 3}}, true},
		{"Ünï", "ünïcode", []Match{{0, 3}}, true},
		{"some", "som", nil, false},
		{"x", "", nil, false},
		{"", "abc", []Match{}, true},
	}
	for _, tt := range tests {
		got, ok := MatchesPrefix(tt.word, tt.target)
		if ok != tt.ok || !slices.Equal(got, tt.want) {
			t.Errorf("MatchesPrefix(%q, %q) = %v, %v", tt.word, tt.target, got, ok)
		}
	}
	if got := CreateMatches([]int{0, 1, 2, 5, 7, 8}); !slices.Equal(got, []Match{{0, 3}, {5, 6}, {7, 9}}) {
		t.Errorf("CreateMatches = %v", got)
	}
	if got := CreateMatches(nil); len(got) != 0 {
		t.Errorf("CreateMatches(nil) = %v", got)
	}
}

func TestCompareFallbackOrder(t *testing.T) {
	// equal scores and lengths go by the names: the ones that start with the query first, then by
	// the collation, which is numeric for file names
	q := PrepareQuery("zz")
	items := []string{"d/zz10.txt", "d/zz9.txt", "d/zz2.txt"}
	slices.SortStableFunc(items, func(a, b string) int {
		return CompareItemsByFuzzyScore[string](nil, a, b, q, true, pathAccessor{}, nil)
	})
	// shorter names first, then the collation of the names
	want := []string{"d/zz2.txt", "d/zz9.txt", "d/zz10.txt"}
	if !slices.Equal(items, want) {
		t.Errorf("sorted = %v, want %v", items, want)
	}
	if got := CompareItemsByFuzzyScore[string](nil, "d/zz2.txt", "d/zz2.txt", q, true, pathAccessor{}, nil); got != 0 {
		t.Errorf("equal items compare as %d", got)
	}
}

func TestCacheSharesNoMatches(t *testing.T) {
	cache := NewCacheSize(10)
	q := PrepareQuery("zzz")
	for _, p := range []string{"a/b.go", "c/d.go"} {
		if res := ScoreItemFuzzy[string](nil, p, q, true, pathAccessor{}, cache); res.Score != 0 {
			t.Fatalf("%q scored", p)
		}
	}
	if cache.Len() != 2 {
		t.Errorf("%d entries, want 2", cache.Len())
	}
	// a hit returns the same result for the same input, and another for another input
	q2 := PrepareQuery("b")
	if res := ScoreItemFuzzy[string](nil, "a/b.go", q2, true, pathAccessor{}, cache); res.Score == 0 {
		t.Error("b should match b.go: the query is part of the key")
	}
}
