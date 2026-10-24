package fuzzy

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/collate"
)

// CompareItemsByFuzzyScore compares two items by their score against a query: negative when a
// goes first, positive when b goes first, zero when they are equal. Identical paths go first,
// then matches on the label over matches on the description, then compact matches, then
// short labels, descriptions and paths; the last resort is a comparison of the names.
// s may be nil, which uses a new Scorer for the call.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (compareItemsByFuzzyScore)
func CompareItemsByFuzzyScore[T any](s *Scorer, itemA, itemB T, query *PreparedQuery, allowNonContiguousMatches bool, accessor ItemAccessor[T], cache *Cache) int {
	if s == nil {
		s = new(Scorer)
	}
	itemScoreA := ScoreItemFuzzy(s, itemA, query, allowNonContiguousMatches, accessor, cache)
	itemScoreB := ScoreItemFuzzy(s, itemB, query, allowNonContiguousMatches, accessor, cache)

	scoreA := itemScoreA.Score
	scoreB := itemScoreB.Score

	// 1.) identity matches have highest score
	if scoreA == PathIdentityScore || scoreB == PathIdentityScore {
		if scoreA != scoreB {
			if scoreA == PathIdentityScore {
				return -1
			}
			return 1
		}
	}

	// 2.) matches on label are considered higher compared to label+description matches
	if scoreA > LabelScoreThreshold || scoreB > LabelScoreThreshold {
		if scoreA != scoreB {
			if scoreA > scoreB {
				return -1
			}
			return 1
		}

		// prefer more compact matches over longer in label (unless this is a prefix match where
		// longer prefix matches are actually preferred)
		if scoreA < LabelPrefixScoreThreshold && scoreB < LabelPrefixScoreThreshold {
			if comparedByMatchLength := compareByMatchLength(itemScoreA.LabelMatch, itemScoreB.LabelMatch); comparedByMatchLength != 0 {
				return comparedByMatchLength
			}
		}

		// prefer shorter labels over longer labels
		labelA := accessor.ItemLabel(itemA)
		labelB := accessor.ItemLabel(itemB)
		if la, lb := utf8.RuneCountInString(labelA), utf8.RuneCountInString(labelB); la != lb {
			return la - lb
		}
	}

	// 3.) compare by score in label+description
	if scoreA != scoreB {
		if scoreA > scoreB {
			return -1
		}
		return 1
	}

	// 4.) scores are identical: prefer matches in label over non-label matches
	itemAHasLabelMatches := len(itemScoreA.LabelMatch) > 0
	itemBHasLabelMatches := len(itemScoreB.LabelMatch) > 0
	if itemAHasLabelMatches && !itemBHasLabelMatches {
		return -1
	} else if itemBHasLabelMatches && !itemAHasLabelMatches {
		return 1
	}

	// 5.) scores are identical: prefer more compact matches (label and description)
	itemAMatchDistance := computeLabelAndDescriptionMatchDistance(itemA, itemScoreA, accessor)
	itemBMatchDistance := computeLabelAndDescriptionMatchDistance(itemB, itemScoreB, accessor)
	if itemAMatchDistance != 0 && itemBMatchDistance != 0 && itemAMatchDistance != itemBMatchDistance {
		if itemBMatchDistance > itemAMatchDistance {
			return -1
		}
		return 1
	}

	// 6.) scores are identical: start to use the fallback compare
	return fallbackCompare(s, itemA, itemB, query, accessor)
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (computeLabelAndDescriptionMatchDistance)
func computeLabelAndDescriptionMatchDistance[T any](item T, score ItemScore, accessor ItemAccessor[T]) int {
	matchStart := -1
	matchEnd := -1

	// If we have description matches, the start is first of description match
	if len(score.DescriptionMatch) > 0 {
		matchStart = score.DescriptionMatch[0].Start
	} else if len(score.LabelMatch) > 0 {
		// Otherwise, the start is the first label match
		matchStart = score.LabelMatch[0].Start
	}

	// If we have label match, the end is the last label match
	// If we had a description match, we add the length of the description
	// as offset to the end to indicate this.
	if len(score.LabelMatch) > 0 {
		matchEnd = score.LabelMatch[len(score.LabelMatch)-1].End
		if len(score.DescriptionMatch) > 0 {
			if itemDescription := accessor.ItemDescription(item); itemDescription != "" {
				matchEnd += utf8.RuneCountInString(itemDescription)
			}
		}
	} else if len(score.DescriptionMatch) > 0 {
		// If we have just a description match, the end is the last description match
		matchEnd = score.DescriptionMatch[len(score.DescriptionMatch)-1].End
	}

	return matchEnd - matchStart
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (compareByMatchLength)
func compareByMatchLength(matchesA, matchesB []Match) int {
	if len(matchesA) == 0 && len(matchesB) == 0 {
		return 0 // make sure to not cause bad comparing when matches are not provided
	}

	if len(matchesB) == 0 {
		return -1
	}

	if len(matchesA) == 0 {
		return 1
	}

	// Compute match length of A (first to last match)
	matchStartA := matchesA[0].Start
	matchEndA := matchesA[len(matchesA)-1].End
	matchLengthA := matchEndA - matchStartA

	// Compute match length of B (first to last match)
	matchStartB := matchesB[0].Start
	matchEndB := matchesB[len(matchesB)-1].End
	matchLengthB := matchEndB - matchStartB

	// Prefer shorter match length
	if matchLengthA == matchLengthB {
		return 0
	}
	if matchLengthB < matchLengthA {
		return 1
	}
	return -1
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (fallbackCompare)
func fallbackCompare[T any](s *Scorer, itemA, itemB T, query *PreparedQuery, accessor ItemAccessor[T]) int {

	// check for label + description length and prefer shorter
	labelA := accessor.ItemLabel(itemA)
	labelB := accessor.ItemLabel(itemB)

	descriptionA := accessor.ItemDescription(itemA)
	descriptionB := accessor.ItemDescription(itemB)

	labelDescriptionALength := utf8.RuneCountInString(labelA) + utf8.RuneCountInString(descriptionA)
	labelDescriptionBLength := utf8.RuneCountInString(labelB) + utf8.RuneCountInString(descriptionB)

	if labelDescriptionALength != labelDescriptionBLength {
		return labelDescriptionALength - labelDescriptionBLength
	}

	// check for path length and prefer shorter
	pathA := accessor.ItemPath(itemA)
	pathB := accessor.ItemPath(itemB)

	if pathA != "" && pathB != "" {
		if la, lb := utf8.RuneCountInString(pathA), utf8.RuneCountInString(pathB); la != lb {
			return la - lb
		}
	}

	// 7.) finally we have equal scores and equal length, we fallback to comparer

	// compare by label
	if labelA != labelB {
		return s.compareAnything(labelA, labelB, query.Normalized)
	}

	// compare by description
	if descriptionA != "" && descriptionB != "" && descriptionA != descriptionB {
		return s.compareAnything(descriptionA, descriptionB, query.Normalized)
	}

	// compare by path
	if pathA != "" && pathB != "" && pathA != pathB {
		return s.compareAnything(pathA, pathB, query.Normalized)
	}

	// equal
	return 0
}

// compareAnything compares two strings that were found by a query: the strings that start
// with the query first (the shorter first), then the ones that end with it, then by the file
// name rules, then by the collation of the root locale.
//
// Ported from VS Code: src/vs/base/common/comparers.ts (compareAnything). VS Code collates
// with Intl.Collator of the locale of the user; the root collation of golang.org/x/text
// (CLDR root) is used here.
func (s *Scorer) compareAnything(one, other, lookFor string) int {
	elementAName := strings.ToLower(one)
	elementBName := strings.ToLower(other)

	// Sort prefix matches over non prefix matches
	if prefixCompare := compareByPrefix(one, other, lookFor); prefixCompare != 0 {
		return prefixCompare
	}

	// Sort suffix matches over non suffix matches
	elementASuffixMatch := strings.HasSuffix(elementAName, lookFor)
	elementBSuffixMatch := strings.HasSuffix(elementBName, lookFor)
	if elementASuffixMatch != elementBSuffixMatch {
		if elementASuffixMatch {
			return -1
		}
		return 1
	}

	plain, base := s.collators()

	// Understand file names
	if r := compareFileNames(base, elementAName, elementBName); r != 0 {
		return r
	}

	// Compare by name
	return plain.CompareString(elementAName, elementBName)
}

// Ported from VS Code: src/vs/base/common/comparers.ts (compareByPrefix)
func compareByPrefix(one, other, lookFor string) int {
	elementAName := strings.ToLower(one)
	elementBName := strings.ToLower(other)

	// Sort prefix matches over non prefix matches
	elementAPrefixMatch := strings.HasPrefix(elementAName, lookFor)
	elementBPrefixMatch := strings.HasPrefix(elementBName, lookFor)
	if elementAPrefixMatch != elementBPrefixMatch {
		if elementAPrefixMatch {
			return -1
		}
		return 1
	}

	// Same prefix: Sort shorter matches to the top to have those on top that match more precisely
	if elementAPrefixMatch && elementBPrefixMatch {
		la, lb := utf8.RuneCountInString(elementAName), utf8.RuneCountInString(elementBName)
		if la < lb {
			return -1
		}
		if la > lb {
			return 1
		}
	}

	return 0
}

// compareFileNames compares file names without distinguishing the name from the extension,
// numerically and ignoring case and accents, and disambiguates by code points.
//
// Ported from VS Code: src/vs/base/common/comparers.ts (compareFileNames)
func compareFileNames(base *collate.Collator, a, b string) int {
	result := base.CompareString(a, b)

	// Using the numeric option will make compare(`foo1`, `foo01`) === 0. Disambiguate.
	if result == 0 && a != b {
		return strings.Compare(a, b)
	}

	return result
}
