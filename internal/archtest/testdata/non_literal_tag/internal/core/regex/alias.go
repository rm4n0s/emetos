package regex

import xerr "github.com/rm4n0s/errors"

func formatted(t string) error {
	return xerr.NewErrf(t, "%d", 1)
}
