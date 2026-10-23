package regex

import (
	"testing"
	"time"
)

// FuzzCompile checks that no pattern makes Compile panic or run for long, and that a pattern that
// compiles can be searched.
func FuzzCompile(f *testing.F) {
	for _, seed := range []string{
		`a(?i)b|c`, `(?<n>a)\k<n>`, `[[:alpha:]&&[^a]]+`, `(\((?:[^()]|\g<1>)*\))`, `(?x) a # b`,
		`\p{^Alpha}\h\H`, `a{2,1}+`, `(?~x)`, `(?(1)a|b)(c)`, `\x{41 42}`, `(?<=a|bc)d`, `\G\A^$`, `[a-\d]`,
		`\g<0>`, `\Kx`, `((((((((((a))))))))))\10`, `(?:(?:(?:a*)*)*)*b`, `\k<-1>(a)`, `[\x{10FFFF}-\x{10FFFF}]`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, pattern string) {
		re, err := Compile(pattern, Options{Timeout: 50 * time.Millisecond})
		if err != nil {
			return
		}
		// A timeout is an error and not a failure.
		_, _, _ = re.Find([]rune("aabc (a(b)) 12 xyz\n"), 0)
	})
}
