// Code generated from VS Code's textModel.test.ts by a script that is not kept; DO NOT EDIT.
// Ported from VS Code: reference/vscode/src/vs/editor/test/common/model/textModel.test.ts (MIT)
package indent

// guessCase is one call of VS Code's assertGuess: the insertSpaces and tabSize that guessIndentation must find,
// where unknown (undefined in the test) means it keeps the default, and tabSizeOnlyForSpaces is the [n] form.
type guessCase struct {
	name                 string
	insertSpaces         string // "true", "false" or "undefined"
	tabSize              int    // 0 for undefined
	tabSizeOnlyForSpaces bool
	lines                []string
}

var guessCases = []guessCase{
	{name: "guess indentation 1: no clues", insertSpaces: "undefined", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"x", "x", "x", "x", "x", "x", "x"}},
	{name: "guess indentation 1: no spaces, 1xTAB", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", "x", "x", "x", "x", "x", "x"}},
	{name: "guess indentation 1: 1x2", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"  x", "x", "x", "x", "x", "x", "x"}},
	{name: "guess indentation 1: 7xTAB", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", "\tx", "\tx", "\tx", "\tx", "\tx", "\tx"}},
	{name: "guess indentation 1: 4x2, 4xTAB", insertSpaces: "undefined", tabSize: 2, tabSizeOnlyForSpaces: true, lines: []string{"\tx", "  x", "\tx", "  x", "\tx", "  x", "\tx", "  x"}},
	{name: "guess indentation 1: 4x1, 4xTAB", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", " x", "\tx", " x", "\tx", " x", "\tx", " x"}},
	{name: "guess indentation 1: 4x2, 5xTAB", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", "\tx", "  x", "\tx", "  x", "\tx", "  x", "\tx", "  x"}},
	{name: "guess indentation 1: 1x2, 5xTAB", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", "\tx", "x", "\tx", "x", "\tx", "x", "\tx", "  x"}},
	{name: "guess indentation 1: 1x4, 5xTAB", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", "\tx", "x", "\tx", "x", "\tx", "x", "\tx", "    x"}},
	{name: "guess indentation 1: 1x2, 1x4, 5xTAB", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", "\tx", "x", "\tx", "x", "\tx", "  x", "\tx", "    x"}},
	{name: "guess indentation 1: 7x1 - 1 space is never guessed as an indentation", insertSpaces: "undefined", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"x", " x", " x", " x", " x", " x", " x", " x"}},
	{name: "guess indentation 1: 1x10, 6x1", insertSpaces: "true", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"x", "          x", " x", " x", " x", " x", " x", " x"}},
	{name: "guess indentation 1: whitespace lines don't count", insertSpaces: "undefined", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"", "  ", "    ", "      ", "        ", "          ", "            ", "              "}},
	{name: "guess indentation 1: 6x3, 3x4", insertSpaces: "true", tabSize: 3, tabSizeOnlyForSpaces: false, lines: []string{"x", "   x", "   x", "    x", "x", "   x", "   x", "    x", "x", "   x", "   x", "    x"}},
	{name: "guess indentation 1: 6x5, 3x4", insertSpaces: "true", tabSize: 5, tabSizeOnlyForSpaces: false, lines: []string{"x", "     x", "     x", "    x", "x", "     x", "     x", "    x", "x", "     x", "     x", "    x"}},
	{name: "guess indentation 1: 6x7, 1x5, 2x4", insertSpaces: "true", tabSize: 7, tabSizeOnlyForSpaces: false, lines: []string{"x", "       x", "       x", "     x", "x", "       x", "       x", "    x", "x", "       x", "       x", "    x"}},
	{name: "guess indentation 1: 8x2", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "  x", "  x", "  x", "x", "  x", "  x", "  x", "  x"}},
	{name: "guess indentation 1: 8x2", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "  x", "x", "  x", "  x", "x", "  x", "  x", "x", "  x", "  x"}},
	{name: "guess indentation 1: 4x2, 4x4", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "    x", "x", "  x", "    x", "x", "  x", "    x", "x", "  x", "    x"}},
	{name: "guess indentation 1: 6x2, 3x4", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "  x", "    x", "x", "  x", "  x", "    x", "x", "  x", "  x", "    x"}},
	{name: "guess indentation 1: 4x2, 4x4", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "  x", "    x", "    x", "x", "  x", "  x", "    x", "    x"}},
	{name: "guess indentation 1: 2x2, 4x4", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "    x", "    x", "x", "  x", "    x", "    x"}},
	{name: "guess indentation 1: 8x4", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"x", "    x", "    x", "x", "    x", "    x", "x", "    x", "    x", "x", "    x", "    x"}},
	{name: "guess indentation 1: 2x2, 4x4, 2x6", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "    x", "    x", "      x", "x", "  x", "    x", "    x", "      x"}},
	{name: "guess indentation 1: 1x2, 2x4, 2x6, 1x8", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  x", "    x", "    x", "      x", "      x", "        x"}},
	{name: "guess indentation 1: 6x4, 2x5, 2x8", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"x", "    x", "    x", "    x", "     x", "        x", "x", "    x", "    x", "    x", "     x", "        x"}},
	{name: "guess indentation 1: 3x4, 1x5, 2x8", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"x", "    x", "    x", "    x", "     x", "        x", "        x"}},
	{name: "guess indentation 1: 6x4, 2x5, 4x8", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"x", "x", "    x", "    x", "     x", "        x", "        x", "x", "x", "    x", "    x", "     x", "        x", "        x"}},
	{name: "guess indentation 1: 5x1, 2x0, 1x3, 2x4", insertSpaces: "true", tabSize: 3, tabSizeOnlyForSpaces: false, lines: []string{"x", " x", " x", " x", " x", " x", "x", "   x", "    x", "    x"}},
	{name: "guess indentation 1: mixed whitespace 1", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\t x", " \t x", "\tx"}},
	{name: "guess indentation 1: mixed whitespace 2", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"\tx", "\t    x"}},
	{name: "issue #44991: Wrong indentation size auto-detection", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"a = 10             # 0 space indent", "b = 5              # 0 space indent", "if a > 10:         # 0 space indent", "    a += 1         # 4 space indent      delta 4 spaces", "    if b > 5:      # 4 space indent", "        b += 1     # 8 space indent      delta 4 spaces", "        b += 1     # 8 space indent", "        b += 1     # 8 space indent", "# comment line 1   # 0 space indent      delta 8 spaces", "# comment line 2   # 0 space indent", "# comment line 3   # 0 space indent", "        b += 1     # 8 space indent      delta 8 spaces", "        b += 1     # 8 space indent", "        b += 1     # 8 space indent"}},
	{name: "issue #55818: Broken indentation detection", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"", "/* REQUIRE */", "", "const foo = require ( 'foo' ),", "      bar = require ( 'bar' );", "", "/* MY FN */", "", "function myFn () {", "", "  const asd = 1,", "        dsa = 2;", "", "  return bar ( foo ( asd ) );", "", "}", "", "/* EXPORT */", "", "module.exports = myFn;", ""}},
	{name: "issue #70832: Broken indentation detection", insertSpaces: "false", tabSize: 0, tabSizeOnlyForSpaces: false, lines: []string{"x", "x", "x", "x", "\tx", "\t\tx", "    x", "\t\tx", "\tx", "\t\tx", "\tx", "\tx", "\tx", "\tx", "x"}},
	{name: "issue #62143: Broken indentation detection", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "x", "  x", "  x"}},
	{name: "issue #62143: Broken indentation detection", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"x", "  - item2", "  - item3"}},
	{name: "issue #84217: Broken indentation detection", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"def main():", "    print('hello')"}},
	{name: "issue #84217: Broken indentation detection", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"def main():", "    with open('foo') as fp:", "        print(fp.read())"}},
	{name: "issue #65668: YAML file indented with 2 spaces", insertSpaces: "true", tabSize: 2, tabSizeOnlyForSpaces: false, lines: []string{"version: 2", "", "jobs:", "  build:", "    docker:", "      - circleci/golang:1.11", "", "  environment:", "    TEST_RESULTS: /tmp/test-results", "", "  steps:", "    - checkout", "    - run: mkdir -p $TEST_RESULTS", "", "    - restore_cache:", "        keys:", "          - v1-pkg-cache", "", "    - run:", "        name: dep ensure", "        command: dep ensure -v", "", "    - run:", "        name: Run unit tests", "        command: |", "          trap \"go-junit-report <${TEST_RESULTS}/go-test.out > ${TEST_RESULTS}/go-test-report.xml\" EXIT", "          go test -v ./... | tee ${TEST_RESULTS}/go-test.out", "", "    - run:", "        name: Build", "        command: go build -v", "", "    - save_cache:", "        key: v1-pkg-cache", "        paths:", "          - \"/go/pkg\"", "", "    - store_artifacts:", "        path: /tmp/test-results", "        destination: raw-test-output", "", "    - store_test_results:", "        path: /tmp/test-results"}},
	{name: "issue #249040: 4-space indent should win over 2-space when predominant", insertSpaces: "true", tabSize: 4, tabSizeOnlyForSpaces: false, lines: []string{"function foo() {", "    let a = 1;", "    let b = 2;", "    if (true) {", "        console.log(a);", "        console.log(b);", "    }", "    const obj = {", "      x: 1,", "      y: 2", "    };", "    return obj;", "}"}},
}
