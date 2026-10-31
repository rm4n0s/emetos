package gitscm

import (
	"os"

	"github.com/rm4n0s/glutter/foundation"
)

// DevTrustEnv is the environment variable that trusts the repositories below a folder for the session, so that the question does not
// cover a proof image (debug builds and builds with the emetos_dev tag only, SPEC §15.2). A release build never reads it. Nothing is
// written to the trust file.
const DevTrustEnv = "EMETOS_DEV_GIT_TRUST"

// devTrustDir is the folder the knob names, or "" in a release build and when the variable is not set.
func devTrustDir(opts Options) string {
	if opts.DevTrust != "" {
		return opts.DevTrust
	}
	if !foundation.DebugMode && !devBuild {
		return ""
	}
	return os.Getenv(DevTrustEnv)
}
