package emmet

import (
	"github.com/rm4n0s/errors"
	"os"
)

type parser struct{ pos int }

// fail only builds the error for its callers: a helper.
func (p *parser) fail(msg string) error {
	return errors.New("ParseFailed", msg, "pos", p.pos)
}

// errNoWindow is a helper that panics use.
func errNoWindow() error {
	return errors.New("WindowMissing", "no window")
}

// wrap is a helper with argument computation and a SetMetadata chain.
func wrap(err error, path string) *errors.Error {
	where := "at " + path
	return errors.NewErr("WrapFailed", err).SetMetadata("where", where)
}

func (p *parser) first() error {
	return p.fail("first")
}

func must() {
	panic(errNoWindow())
}

func open(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return wrap(err, path)
	}
	return f.Close()
}

// detects tests something and then builds the error: not a helper.
func (p *parser) detects(n int) error {
	if n < 0 {
		return errors.New("Negative", "negative", "n", n)
	}
	return nil
}

// stub always fails and nobody returns a call of it: the place of its failure, not a helper.
func newInotify() (*parser, error) {
	return nil, errors.New("WatcherInitFailed", "only on Linux")
}

func start() (*parser, error) {
	p, err := newInotify()
	if err != nil {
		return nil, err
	}
	return p, nil
}

func closures() error {
	fail := func(err error) error {
		return errors.NewErr("ClosureFailed", err)
	}
	if _, err := os.Open("x"); err != nil {
		return fail(err)
	}
	return nil
}

// A closure that is passed on as a handler is not a helper.
func register(set func(func() error)) {
	set(func() error { return errors.New("NoEditor", "no editor") })
}
