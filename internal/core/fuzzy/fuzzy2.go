package fuzzy

// Score2 is the result of ScoreFuzzy2.
type Score2 struct {
	// Matched is false when the target does not match (all pieces of the query have to).
	Matched bool
	// Score is the score of the match.
	Score int
	// Matches are the matching ranges of the target, sorted, with no overlap.
	Matches []Match
}

// ScoreFuzzy2 is the alternate scorer that VS Code uses for symbols. It matches a query (every
// piece of it) against the target with the word scorer, FuzzyScore. patternStart is the offset
// in the query and wordStart the offset in the target where matching starts; the matches are
// offsets in the target.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (scoreFuzzy2)
func (s *Scorer) ScoreFuzzy2(target string, query *PreparedQuery, patternStart, wordStart int) Score2 {
	t := appendRunes(s.tgt[:0], target)
	tl := appendLower(s.tgtLow[:0], t)
	s.tgt, s.tgtLow = t, tl

	// Score: multiple inputs
	if len(query.Values) > 1 {
		return s.doScoreFuzzy2Multiple(t, tl, query.Values, patternStart, wordStart)
	}

	// Score: single input
	return s.doScoreFuzzy2Single(t, tl, &query.PreparedQueryPiece, patternStart, wordStart)
}

// ScoreFuzzy2 is Scorer.ScoreFuzzy2 with a new Scorer.
func ScoreFuzzy2(target string, query *PreparedQuery, patternStart, wordStart int) Score2 {
	return new(Scorer).ScoreFuzzy2(target, query, patternStart, wordStart)
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (doScoreFuzzy2Multiple)
func (s *Scorer) doScoreFuzzy2Multiple(target, targetLow []rune, query []PreparedQueryPiece, patternStart, wordStart int) Score2 {
	totalScore := 0
	var totalMatches []Match

	for i := range query {
		res := s.doScoreFuzzy2Single(target, targetLow, &query[i], patternStart, wordStart)
		if !res.Matched {
			// if a single query value does not match, return with
			// no score entirely, we require all queries to match
			return Score2{}
		}

		totalScore += res.Score
		totalMatches = append(totalMatches, res.Matches...)
	}

	// if we have a score, ensure that the positions are
	// sorted in ascending order and distinct
	return Score2{Matched: true, Score: totalScore, Matches: normalizeMatches(totalMatches)}
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (doScoreFuzzy2Single)
func (s *Scorer) doScoreFuzzy2Single(target, targetLow []rune, query *PreparedQueryPiece, patternStart, wordStart int) Score2 {
	qn, ql := query.runes(&s.item.q)
	if len(qn) != len(ql) {
		return Score2{}
	}
	score, ok := s.fuzzyScoreRunes(qn, ql, patternStart, target, targetLow, wordStart, FuzzyScoreOptions{FirstMatchCanBeWeak: true, BoostFullMatch: true})
	if !ok {
		return Score2{}
	}
	return Score2{Matched: true, Score: score.Score, Matches: score.Matches()}
}
