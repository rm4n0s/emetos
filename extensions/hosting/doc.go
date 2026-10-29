// Package hosting is the extension emetos.hosting: the Sharing status item of every workspace (SPEC §10.29). It says whether Remote
// Work hosts the workspace, and its menu starts and stops it, so that nobody has to find a page or a tab to know or to change it.
// The item reads the hosting extension through extensions/internal/sharing; the extension itself keeps the listener, the keys and
// the commands (emetos.remotework.host, emetos.remotework.stopHosting, ...) that the menu runs.
package hosting
