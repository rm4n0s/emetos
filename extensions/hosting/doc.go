// Package hosting is the extension emetos.hosting: the Sharing status item of every workspace (SPEC §10.29). It says whether Remote
// Work or Relay hosts the workspace, and its menu starts and stops both, so that nobody has to find a page or a tab to know or to
// change it. The item reads the two extensions through extensions/internal/sharing; the extensions themselves keep the listeners, the
// keys and the commands (emetos.remotework.host, emetos.relay.stopHosting, ...) that the menu runs.
package hosting
