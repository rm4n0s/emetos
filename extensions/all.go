package extensions

import "github.com/rm4n0s/emetos/extension"

// All returns the constructors of the extensions compiled into Emetos, in activation order. It makes a new slice at each
// call, so nobody can change the list; there is no registration from init functions. To add an extension, add its folder
// and one line here.
//
// The list is empty at step 11.1: the bundled languages (step 11.4) and the extensions of the later steps are added as
// they are written. The test extension of this contract is extensions/internal/testext; it is never in the list.
func All() []extension.Factory {
	return []extension.Factory{}
}
