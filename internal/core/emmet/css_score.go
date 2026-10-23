package emmet

// Ported from Emmet: src/stylesheet/score.ts

// asciiLower lowercases the letters A to Z only, so that the bytes keep their places.
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] |= 32
				}
			}
			return string(b)
		}
	}
	return s
}

// scoreMatch tells how close str1 matches str2 with a fuzzy match, from 0 (it does not) to 1 (equal).
// How the matching works:
//   - the first characters of both must be the same
//   - a str1 longer than str2 is allowed only with partialMatch
//   - an ideal match is str1 equal to str2 (score 1)
//   - the next best is str2 starts with str1 (1 times the share of the matched characters)
//   - the other scores depend on how close the characters of str1 are to the start of str2
func scoreMatch(str1, str2 string, partialMatch bool) float64 {
	str1 = asciiLower(str1)
	str2 = asciiLower(str2)

	if str1 == str2 {
		return 1
	}

	// Both strings must start with the same character
	if str1 == "" || str2 == "" || str1[0] != str2[0] {
		return 0
	}

	str1Len, str2Len := len(str1), len(str2)
	if !partialMatch && str1Len > str2Len {
		return 0
	}

	// The characters of str1 that are closer to the start of str2 have a higher score. For str2
	// "abcde" the maximum is 5 + 4 + 3 + 2 + 1 = 15 (the sum of the positions in reverse
	// order). Matching "abd" against it gives 5 + 4 + 2 = 11. There is a bonus for an acronym: a
	// match right after a `-`. Matching "abd" against "abc-de" gives 6 + 5 + 4 (the position of
	// the d in "abd", not in "abc-de").
	minLength := min(str1Len, str2Len)
	maxLength := max(str1Len, str2Len)
	i, j := 1, 1
	score := maxLength

	for i < str1Len {
		ch1 := str1[i]
		found := false
		acronym := false

		for j < str2Len {
			ch2 := str2[j]
			if ch1 == ch2 {
				found = true
				if acronym {
					score += maxLength - i
				} else {
					score += maxLength - j
				}
				break
			}
			// The bonus for exactly the next match after an unmatched `-`
			acronym = ch2 == '-'
			j++
		}

		if !found {
			if !partialMatch {
				return 0
			}
			break
		}
		i++
	}

	matchRatio := float64(i) / float64(maxLength)
	delta := maxLength - minLength
	maxScore := sumTo(maxLength) - sumTo(delta)
	return float64(score) * matchRatio / float64(maxScore)
}

// sumTo is the sum of the first n numbers: 1 + 2 + 3 + ... + n.
func sumTo(n int) int { return n * (n + 1) / 2 }
