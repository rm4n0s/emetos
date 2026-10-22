package fileio

// No fmt import: a local value called fmt is not the package.
type formatter struct{}

func (formatter) Errorf() {}

func use() {
	fmt := formatter{}
	fmt.Errorf()
}
