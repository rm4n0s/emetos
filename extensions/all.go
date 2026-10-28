package extensions

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/cpp"
	"github.com/rm4n0s/emetos/extensions/css"
	"github.com/rm4n0s/emetos/extensions/diff"
	"github.com/rm4n0s/emetos/extensions/docker"
	"github.com/rm4n0s/emetos/extensions/git"
	"github.com/rm4n0s/emetos/extensions/golang"
	"github.com/rm4n0s/emetos/extensions/gomod"
	"github.com/rm4n0s/emetos/extensions/hosting"
	"github.com/rm4n0s/emetos/extensions/html"
	"github.com/rm4n0s/emetos/extensions/ini"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
	"github.com/rm4n0s/emetos/extensions/javascript"
	"github.com/rm4n0s/emetos/extensions/json"
	"github.com/rm4n0s/emetos/extensions/less"
	"github.com/rm4n0s/emetos/extensions/makefile"
	"github.com/rm4n0s/emetos/extensions/markdown"
	"github.com/rm4n0s/emetos/extensions/relay"
	"github.com/rm4n0s/emetos/extensions/python"
	"github.com/rm4n0s/emetos/extensions/references"
	"github.com/rm4n0s/emetos/extensions/remotework"
	"github.com/rm4n0s/emetos/extensions/rust"
	"github.com/rm4n0s/emetos/extensions/scss"
	"github.com/rm4n0s/emetos/extensions/search"
	"github.com/rm4n0s/emetos/extensions/shellscript"
	"github.com/rm4n0s/emetos/extensions/sql"
	"github.com/rm4n0s/emetos/extensions/tasks"
	"github.com/rm4n0s/emetos/extensions/terminal"
	"github.com/rm4n0s/emetos/extensions/typescript"
	"github.com/rm4n0s/emetos/extensions/xml"
	"github.com/rm4n0s/emetos/extensions/yaml"
)

// All returns the constructors of the extensions compiled into Emetos, in activation order. It makes a new slice at each
// call, so nobody can change the list; there is no registration from init functions. To add an extension, add its folder
// and one line here.
//
// The language packs come first, in the order the languages are registered (SPEC §8.1, D13: Go, Markdown, JSON, YAML,
// Python, JavaScript and TypeScript, then the rest): the order of the list is the order of registration, which decides
// which language wins when two claim the same file name. internal/core/language/packfs lists the same packs in the same
// order for the tests, and a test in cmd/emetos keeps the two the same. The test extension of the contract is
// extensions/internal/testext; it is never in the list. The extensions of the workbench follow the packs: the Tasks tab of
// the panel (extensions/tasks) and the Terminal tab of the panel (extensions/terminal), then Remote Work (extensions/remotework, whose instances share
// one hub made here, with the Source of the real cameras, which opens no device before the Scan QR Code dialog does) and Relay
// (extensions/relay, the only extension the host grants the privileges events.publishAny and commands.execute; its instances
// share one hub made here too, with the Source of the real cameras), then the Sharing status item (extensions/hosting, step 17e.4),
// which reads how the two hubs host the workspace.
//
// The References view (extensions/references) is last: it adds a view to the side bar and asks the language servers the packs start.
func All() []extension.Factory {
	rw := remotework.NewHub(remotework.HubOptions{Camera: keyqr.NewV4L2()})
	ps := relay.NewHub(relay.HubOptions{Camera: keyqr.NewV4L2()})
	return []extension.Factory{
		golang.New, json.New, yaml.New, markdown.New, python.New, javascript.New, typescript.New,
		html.New, css.New, scss.New, less.New, xml.New, sql.New, shellscript.New, makefile.New, docker.New, ini.New,
		cpp.New, rust.New, diff.New, git.New, gomod.New,
		search.New,
		tasks.New, terminal.New, remotework.FactoryOf(rw), relay.FactoryOf(ps),
		hosting.NewFactory(rw.Hosting(), ps.Hosting()),
		references.New,
	}
}
