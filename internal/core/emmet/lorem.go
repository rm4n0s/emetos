package emmet

import (
	"encoding/json"
	"io/fs"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// Ported from Emmet: src/markup/lorem/index.ts

// MaxLoremWords is the largest number of words `lorem` makes; a larger number is this one.
const MaxLoremWords = 10000

// loremVocabulary is a language of the generator: the first words of the text, and the words.
type loremVocabulary struct {
	Common []string `json:"common"`
	Words  []string `json:"words"`
}

var loremRegex = regexp.MustCompile(`(?i)^lorem([a-z]*)(\d*)(-\d*)?$`)

// loremLanguages are the names of the languages and the files of their vocabularies.
var loremLanguages = map[string]string{"ru": "russian", "sp": "spanish", "latin": "latin"}

// loadVocabulary reads the vocabulary of a language; an unknown language is Latin.
func loadVocabulary(lang string) (*loremVocabulary, error) { return loadVocabularyFrom(assetFS, lang) }

// loadVocabularyFrom reads the vocabulary of a language from a file system.
func loadVocabularyFrom(fsys fs.FS, lang string) (*loremVocabulary, error) {
	file, ok := loremLanguages[lang]
	if !ok {
		file = "latin"
	}
	data, err := fs.ReadFile(fsys, "assets/lorem/"+file+".json")
	if err != nil {
		return nil, errors.NewErr("LoremAssetMissing", err).SetMetadata("language", file)
	}
	var v loremVocabulary
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, errors.NewErr("LoremAssetInvalid", err).SetMetadata("language", file)
	}
	return &v, nil
}

// lorem replaces a node named like `lorem`, `lorem10`, `loremru5-20` by a text of random words.
func lorem(node *Node, ancestors []*Node, cfg *Config) {
	m := loremRegex.FindStringSubmatch(node.Name)
	if node.Name == "" || m == nil {
		return
	}
	db, err := loadVocabulary(m[1])
	if err != nil {
		// The assets are embedded and checked by a test; if one is damaged the node stays as it is
		cfg.warn("Unable to read the vocabulary of Lorem ipsum", err)
		return
	}

	minWords := 30
	if m[2] != "" {
		minWords = max(1, parseCount(m[2]))
	}
	maxWords := minWords
	if m[3] != "" {
		maxWords = max(minWords, parseCount(m[3][1:]))
	}
	minWords = min(minWords, MaxLoremWords)
	maxWords = min(maxWords, MaxLoremWords)
	rng := cfg.randFunc()
	wordCount := randInt(rng, minWords, maxWords)

	repeat := node.Repeat
	if repeat == nil {
		repeat = findRepeater(ancestors)
	}

	node.Name = ""
	node.Attributes = nil
	node.Value = []Value{strValue(paragraph(rng, db, wordCount, repeat == nil || repeat.Value == 0))}

	if node.Repeat != nil && len(ancestors) > 0 {
		resolveImplicitTag(node, ancestors, cfg)
	}
}

// randFunc gives the source of random numbers of the configuration.
func (c *Config) randFunc() func() float64 {
	if c.Rand != nil {
		return c.Rand
	}
	return rand.Float64
}

// randInt gives an integer from..to, the upper bound not included.
func randInt(rng func() float64, from, to int) int {
	return int(math.Floor(rng()*float64(to-from) + float64(from)))
}

func sample(rng func() float64, arr []string, count int) []string {
	n := len(arr)
	iterations := min(n, count)
	result := make([]string, 0, iterations)
	for len(result) < iterations {
		s := arr[min(randInt(rng, 0, n), n-1)]
		if !slices.Contains(result, s) {
			result = append(result, s)
		}
	}
	return result
}

// choice picks one of the characters, but the last (as Emmet's does, which gives more dots than question marks).
func choice(rng func() float64, val string) string {
	return string(val[max(0, min(randInt(rng, 0, len(val)-1), len(val)-1))])
}

func sentence(rng func() float64, words []string, end string) string {
	if len(words) > 0 {
		words = append([]string{capitalize(words[0])}, words[1:]...)
	}
	if end == "" {
		end = choice(rng, "?!...")
	}
	return strings.Join(words, " ") + end
}

func capitalize(word string) string {
	r, size := utf8.DecodeRuneInString(word)
	return string(unicode.ToUpper(r)) + word[size:]
}

// insertCommas puts commas after randomly chosen words, in a copy of the words.
func insertCommas(rng func() float64, words []string) []string {
	if len(words) < 2 {
		return words
	}
	words = slices.Clone(words)
	n := len(words)
	var total int
	switch {
	case n > 3 && n <= 6:
		total = randInt(rng, 0, 1)
	case n > 6 && n <= 12:
		total = randInt(rng, 0, 2)
	default:
		total = randInt(rng, 1, 4)
	}
	for range total {
		pos := randInt(rng, 0, n-2)
		if !strings.HasSuffix(words[pos], ",") {
			words[pos] += ","
		}
	}
	return words
}

// paragraph makes a paragraph of Lorem ipsum of wordCount words; it starts with the common
// sentence of the language if startWithCommon.
func paragraph(rng func() float64, dict *loremVocabulary, wordCount int, startWithCommon bool) string {
	var result []string
	total := 0
	if startWithCommon && dict.Common != nil {
		words := dict.Common[:min(wordCount, len(dict.Common))]
		total += len(words)
		result = append(result, sentence(rng, insertCommas(rng, words), "."))
	}
	for total < wordCount {
		words := sample(rng, dict.Words, min(randInt(rng, 2, 30), wordCount-total))
		if len(words) == 0 {
			break
		}
		total += len(words)
		result = append(result, sentence(rng, insertCommas(rng, words), ""))
	}
	return strings.Join(result, " ")
}

func findRepeater(ancestors []*Node) *Repeater {
	for i := len(ancestors) - 1; i >= 0; i-- {
		if ancestors[i].Repeat != nil {
			return ancestors[i].Repeat
		}
	}
	return nil
}
