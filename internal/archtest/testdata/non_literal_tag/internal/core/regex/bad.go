package regex

import "github.com/rm4n0s/errors"

const tagConst = "FromConst"

func fail(tag, msg string) error {
	if msg == "" {
		return nil
	}
	return errors.New(tag, msg)
}

func wrap(failTag string, err error) error {
	if err == nil {
		return nil
	}
	return errors.NewErr(failTag, err).SetMetadata("a", 1)
}

func fromConst() error {
	if tagConst != "" {
		return errors.New(tagConst, "a constant is not a literal either")
	}
	return errors.New("Literal", "this one is fine")
}
