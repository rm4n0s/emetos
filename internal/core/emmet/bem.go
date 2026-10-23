package emmet

import (
	"regexp"
	"slices"
	"strings"
)

// Ported from Emmet: src/markup/addon/bem.ts

var (
	bemElementRe   = regexp.MustCompile(`(?i)^(-+)([a-z0-9]+[a-z0-9-]*)`)
	bemModifierRe  = regexp.MustCompile(`(?i)^(_+)([a-z0-9]+[a-z0-9_-]*)`)
	bemCandidate1  = regexp.MustCompile(`(?i)^[a-z]-`)
	bemCandidate2  = regexp.MustCompile(`(?i)^[a-z]`)
	bemSpacesRegex = regexp.MustCompile(`\s+`)
)

// bem does the BEM transformation of the class names of a node.
func bem(node *Node, ancestors []*Node, cfg *Config) {
	expandClassNames(node)
	expandShortNotation(node, ancestors, cfg)
}

// expandClassNames expands the class names that are in BEM notation: a node that has `b__el_mod` has
// `b__el` as well.
func expandClassNames(node *Node) {
	data := getBEMData(node)

	var classNames []string
	for _, cl := range data.classNames {
		// Elements and modifiers are removed to get the base name
		if ix := strings.Index(cl, "_"); ix > 0 && !strings.HasPrefix(cl, "-") {
			classNames = append(classNames, cl[:ix], cl[ix:])
		} else {
			classNames = append(classNames, cl)
		}
	}

	if len(classNames) > 0 {
		data.classNames = uniqueClasses(classNames)
		data.block = findBlockName(data.classNames)
		updateClass(node, strings.Join(data.classNames, " "))
	}
}

// expandShortNotation expands the short notation: `-element` and `_modifier`.
func expandShortNotation(node *Node, ancestors []*Node, cfg *Config) {
	data := getBEMData(node)
	var classNames []string
	opts := &cfg.Options
	path := append(append([]*Node{}, ancestors...), node)

	for _, cl := range data.classNames {
		prefix := ""
		original := cl

		// The element (there is only one)
		if m := bemElementRe.FindStringSubmatch(cl); m != nil {
			prefix = getBlockName(path, len(m[1]), cfg.Context) + opts.BEMElement + m[2]
			classNames = append(classNames, prefix)
			cl = cl[len(m[0]):]
		}

		// The modifiers
		if m := bemModifierRe.FindStringSubmatch(cl); m != nil {
			if prefix == "" {
				prefix = getBlockName(path, len(m[1]), nil)
				classNames = append(classNames, prefix)
			}
			classNames = append(classNames, prefix+opts.BEMModifier+m[2])
			cl = cl[len(m[0]):]
		}

		if cl == original {
			// Not BEM: kept as it is
			classNames = append(classNames, original)
		}
	}

	if unique := uniqueClasses(classNames); len(unique) > 0 {
		updateClass(node, strings.Join(unique, " "))
	}
}

// getBEMData gives the BEM data of a node, and keeps it with the node.
func getBEMData(node *Node) *bemData {
	if node.bem == nil {
		classValue := ""
		for _, a := range node.Attributes {
			if a.Name == "class" && a.Value != nil {
				classValue = stringifyBEMValue(a.Value)
				break
			}
		}
		node.bem = parseBEM(classValue)
	}
	return node.bem
}

func parseBEM(classValue string) *bemData {
	var classNames []string
	if classValue != "" {
		classNames = bemSpacesRegex.Split(classValue, -1)
	}
	return &bemData{classNames: classNames, block: findBlockName(classNames)}
}

// getBlockName gives the block of a node: the first block found in the node and its ancestors, depth
// levels up from the end of the path, and then in the context.
func getBlockName(path []*Node, depth int, context *Context) string {
	parentIx := max(len(path)-depth, 0)
	for {
		if parentIx < len(path) {
			if data := getBEMData(path[parentIx]); data.block != "" {
				return data.block
			}
		}
		if parentIx <= 0 {
			break
		}
		parentIx--
	}

	if context != nil {
		if data := parseBEM(context.Attributes["class"]); data.block != "" {
			return data.block
		}
	}
	return ""
}

// findBlockName finds the class name that can be the block.
func findBlockName(classNames []string) string {
	if cl := findClass(classNames, bemCandidate1); cl != "" {
		return cl
	}
	return findClass(classNames, bemCandidate2)
}

func findClass(classNames []string, filter *regexp.Regexp) string {
	for _, cl := range classNames {
		if bemElementRe.MatchString(cl) || bemModifierRe.MatchString(cl) {
			break
		}
		if filter.MatchString(cl) {
			return cl
		}
	}
	return ""
}

func updateClass(node *Node, value string) {
	for _, a := range node.Attributes {
		if a.Name == "class" {
			a.Value = []Value{strValue(value)}
			break
		}
	}
}

func stringifyBEMValue(values []Value) string {
	var sb strings.Builder
	for _, v := range values {
		if v.IsField() {
			sb.WriteString(v.Field.Name)
		} else {
			sb.WriteString(v.Text)
		}
	}
	return sb.String()
}

// uniqueClasses removes the empty names and the repeated ones.
func uniqueClasses(names []string) []string {
	var out []string
	for _, n := range names {
		if n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}
