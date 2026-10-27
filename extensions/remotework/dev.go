package remotework

import (
	"context"
	"math"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
)

// registerDev registers the dev command of the proofs in the scope of a tab, in debug builds and builds with the emetos_dev tag
// only (SPEC §15.2).
func (r *remoteWork) registerDev() error {
	if !foundation.DebugMode && !devBuild {
		return nil
	}
	return r.api.Commands().Register(extension.CommandSpec{
		ID: DevConnectCommand,
		Description: `Fill the connect dialog of the Remote Work tab and press Connect. Arguments: {"host", "port", "keyFile", "workspace"}, ` +
			`or {"host", "port", "agent": true, "fingerprint"}.`,
	}, r.devConnect)
}

// devConnect is dev.remotework.connect: it puts the arguments in the form of the tab and connects, as the user does. It fails with
// RemoteWorkDevConnectInvalid for arguments that are not a host with a key file or the agent.
func (r *remoteWork) devConnect(_ context.Context, args any) error {
	m, _ := args.(map[string]any)
	host, _ := m["host"].(string)
	st := tabState{Host: host, Port: DefaultPort, Key: keyFile}
	if f, ok := m["port"].(float64); ok && f == math.Trunc(f) {
		st.Port = int(f)
	}
	st.KeyFile, _ = m["keyFile"].(string)
	st.Workspace, _ = m["workspace"].(string)
	if agent, _ := m["agent"].(bool); agent {
		st.Key = keyAgent
		st.Fingerprint, _ = m["fingerprint"].(string)
	}
	if host == "" || (st.Key == keyFile && st.KeyFile == "") || len(r.tabs) == 0 {
		return errors.New("RemoteWorkDevConnectInvalid", `dev.remotework.connect needs a Remote Work tab in front and {"host", "keyFile"} or {"host", "agent": true}`, "arguments", args)
	}
	t := r.tabs[len(r.tabs)-1]
	t.form.set(st)
	t.workspac = st.Workspace
	t.connect()
	return nil
}
