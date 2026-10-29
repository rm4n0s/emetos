package containerui

import (
	"regexp"
	"strconv"
	"strings"
)

// The output of `docker build` is read for the problems it reports in a Dockerfile (SPEC §10.20 "Build Image": errors with file:line
// into Problems). BuildKit (docker 23 and later, and `docker buildx`) writes a failed instruction as
//
//	Dockerfile:5
//	--------------------
//	   3 |     WORKDIR /app
//	   5 | >>> RUN nonexistent
//	   6 |
//	--------------------
//	ERROR: failed to solve: process "/bin/sh -c nonexistent" did not complete successfully: exit code: 127
//
// a parse error of the Dockerfile as `dockerfile parse error on line 3: unknown instruction: FOO` (with the block above in newer
// versions), and its checks as ` - FromAsCasing: 'as' and 'FROM' keywords' casing do not match (line 1)`. The classic builder
// writes `Error response from daemon: dockerfile parse error line 3: unknown instruction: FOO`. Podman (Buildah) writes a failed
// instruction as `Error: building at STEP "RUN nonexistent": while running runtime: exit status 127`, with no line: the line is the
// one of that instruction in the file (step 18.4), and a parse error as `Error: parsing Containerfile: dockerfile parse error on line 3: ...`.

// buildProblem is a problem the build reported.
type buildProblem struct {
	// file is the Dockerfile as the output names it ("" when it did not: the Dockerfile that was built), line counts from 1.
	file string
	line int
	// message says what is wrong; warning tells a check of the Dockerfile from an error.
	message string
	warning bool
	// step is the instruction a builder named in place of a line (Buildah): the line is looked for in the file. line is 0 then.
	step string
}

var (
	// positionLine is "Dockerfile:5" or "sub/Dockerfile:12".
	positionLine = regexp.MustCompile(`^(\S.*?):(\d+)$`)
	dashesLine   = regexp.MustCompile(`^-{8,}\s*$`)
	errorLine    = regexp.MustCompile(`^(?:ERROR|Error)(?::| response from daemon:)\s*(.*)$`)
	parseError   = regexp.MustCompile(`dockerfile parse error(?: on)? line (\d+): (.*)$`)
	checkLine    = regexp.MustCompile(`^\s+-\s+(\w+):\s+(.*?)\s+\(line (\d+)\)\s*$`)
	// stepError is the failure of an instruction as Buildah writes it.
	stepError = regexp.MustCompile(`^Error: building at STEP "(.+?)": (.*)$`)
)

// parseBuildOutput finds the problems in the lines of the output of a build.
func parseBuildOutput(lines []string) []buildProblem {
	var out []buildProblem
	seen := map[[2]string]bool{}
	add := func(p buildProblem) {
		k := [2]string{p.file + ":" + strconv.Itoa(p.line), p.message}
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, p)
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], " \t")
		// a block with a position
		if m := positionLine.FindStringSubmatch(line); m != nil && i+1 < len(lines) && dashesLine.MatchString(lines[i+1]) {
			n, _ := strconv.Atoi(m[2])
			j := i + 2
			for j < len(lines) && !dashesLine.MatchString(lines[j]) {
				j++
			}
			j++ // the closing dashes
			if j < len(lines) {
				if em := errorLine.FindStringSubmatch(strings.TrimSpace(lines[j])); em != nil && n > 0 {
					msg := strings.TrimPrefix(em[1], "failed to solve: ")
					if pm := parseError.FindStringSubmatch(msg); pm != nil {
						msg = strings.TrimSpace(pm[2])
					}
					add(buildProblem{file: m[1], line: n, message: msg})
					i = j
					continue
				}
			}
			i += 1
			continue
		}
		if m := parseError.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			if n > 0 {
				add(buildProblem{line: n, message: strings.TrimSpace(m[2])})
			}
			continue
		}
		if m := stepError.FindStringSubmatch(line); m != nil {
			add(buildProblem{step: strings.TrimSpace(m[1]), message: "the step \"" + strings.TrimSpace(m[1]) + "\" failed: " + strings.TrimSpace(m[2])})
			continue
		}
		if m := checkLine.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[3])
			if n > 0 {
				add(buildProblem{line: n, message: m[1] + ": " + m[2], warning: true})
			}
		}
	}
	return out
}
