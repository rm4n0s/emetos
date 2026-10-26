package fuzzy

import (
	"strings"
	"testing"
)

// Tests ported from VS Code: src/vs/base/test/common/filters.test.ts ("fuzzyScoreGraceful", "Suggestion is not highlighted
// #85826", "IntelliSense completion not correctly highlighting text in front of cursor #115250" and "anyScore should not
// require a strong first match").

type variant func(s *Scorer, pattern string, patternPos int, word string) (WordScore, bool)

func TestScorerVariants(t *testing.T) {
	plain := func(s *Scorer, p string, pp int, w string) (WordScore, bool) {
		return s.FuzzyScore(p, strings.ToLower(p), pp, w, strings.ToLower(w), 0, FuzzyScoreOptions{BoostFullMatch: true})
	}
	graceful := func(s *Scorer, p string, pp int, w string) (WordScore, bool) {
		return s.FuzzyScoreGraceful(p, strings.ToLower(p), pp, w, strings.ToLower(w), 0, FuzzyScoreOptions{BoostFullMatch: true})
	}
	aggressive := func(s *Scorer, p string, pp int, w string) (WordScore, bool) {
		return s.FuzzyScoreGracefulAggressive(p, strings.ToLower(p), pp, w, strings.ToLower(w), 0, FuzzyScoreOptions{BoostFullMatch: true})
	}
	anyScore := func(s *Scorer, p string, pp int, w string) (WordScore, bool) {
		r := s.AnyScore(p, strings.ToLower(p), pp, w, strings.ToLower(w), 0)
		return r, len(r.Positions) > 0
	}
	tests := []struct {
		name          string
		scorer        variant
		pattern, word string
		decorated     string // "" is no match
	}{
		{"plain does not forgive rlut", plain, "rlut", "result", ""},
		{"graceful forgives rlut", graceful, "rlut", "result", "^res^u^l^t"},
		{"plain cno", plain, "cno", "console", "^co^ns^ole"},
		{"graceful keeps cno", graceful, "cno", "console", "^co^ns^ole"},
		{"aggressive aligns cno", aggressive, "cno", "console", "^c^o^nsole"},
		{"graceful cno co_new", graceful, "cno", "co_new", "^c^o_^new"},
		{"aggressive cno co_new", aggressive, "cno", "co_new", "^c^o_^new"},
		{"#85826 plain", plain, "SemanticTokens", "SemanticTokensEdits", "^S^e^m^a^n^t^i^c^T^o^k^e^n^sEdits"},
		{"#85826 aggressive", aggressive, "SemanticTokens", "SemanticTokensEdits", "^S^e^m^a^n^t^i^c^T^o^k^e^n^sEdits"},
		{"#115250 plain", plain, "lo", "log", "^l^og"},
		{"#115250 anyScore drops the dot", anyScore, ".lo", "log", "^l^og"},
		{"#115250 anyScore only a dot", anyScore, ".", "log", ""},
		{"anyScore weak first match", anyScore, "bar", "foobAr", "foo^b^A^r"},
		{"anyScore weak first match lower", anyScore, "bar", "foobar", "foo^b^a^r"},
	}
	s := NewScorer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := tt.scorer(s, tt.pattern, 0, tt.word)
			if (tt.decorated != "") != ok {
				t.Fatalf("match = %v, want %v", ok, tt.decorated != "")
			}
			if ok {
				if got := decorate(tt.word, r.Matches()); got != tt.decorated {
					t.Errorf("matches %q, want %q", got, tt.decorated)
				}
			}
		})
	}
}

func TestAnyScoreNeverFails(t *testing.T) {
	r := AnyScore(".", ".", 0, "log", "log", 0)
	if r.Score != 0 || len(r.Positions) != 0 || r.WordStart != 0 {
		t.Errorf("no match gives %+v, want the zero score", r)
	}
}

func TestGracefulPenalty(t *testing.T) {
	s := NewScorer()
	opts := FuzzyScoreOptions{BoostFullMatch: true}
	plain, _ := s.FuzzyScore("res", "res", 0, "result", "result", 0, opts)
	typo, ok := s.FuzzyScoreGraceful("rse", "rse", 0, "result", "result", 0, opts)
	if !ok {
		t.Fatal("no match for the typo")
	}
	if typo.Score >= plain.Score {
		t.Errorf("typo score %d is not below the plain %d", typo.Score, plain.Score)
	}
}

func TestIsPatternInWord(t *testing.T) {
	tests := []struct {
		pattern, word string
		want          bool
	}{
		{"", "abc", true}, {"ac", "abc", true}, {"ca", "abc", false}, {"abcd", "abc", false}, {"é", "café", true},
	}
	for _, tt := range tests {
		if got := IsPatternInWord(tt.pattern, tt.word); got != tt.want {
			t.Errorf("IsPatternInWord(%q, %q) = %v, want %v", tt.pattern, tt.word, got, tt.want)
		}
	}
}
