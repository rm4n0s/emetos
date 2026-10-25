package thing

import "github.com/rm4n0s/errors"

func Do(n int) error {
	switch n {
	case 1:
		return errors.New("LiteralRoute", "asserted with a literal route")
	case 2:
		return errors.New("TableRoute", "asserted by a table of routes")
	case 3:
		return errors.New("TableTag", "asserted by a table of tags")
	case 4:
		return errors.New("SentinelTag", "asserted through errors.Is")
	case 5:
		return errors.New("ConstRoute", "asserted with a constant")
	case 6:
		return errors.New("HelperRoute", "asserted by a helper that builds the route")
	case 7:
		return errors.New("OnlyInComment", "named in a comment")
	case 8:
		return errors.New("OnlyAFake", "made by a fake in a test")
	case 9:
		return errors.New("NeverNamed", "no test names it")
	case 10:
		return errors.NewErr("NeverNamed", nil)
	}
	return nil
}

func (t *Thing) Method() error { return errors.NewErrf("MethodTag", "never named %d", 1) }

type Thing struct{}
