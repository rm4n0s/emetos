package javascript

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// newNoTestingAPI makes the fake API of a window that has no Testing view, with the manifest of the extension, over the folder.
func newNoTestingAPI(t *testing.T, ext *javascript, folder string) *fakeapi.API {
	t.Helper()
	m := ext.Manifest()
	m.Contributes.Settings = append(m.Contributes.Settings, extension.SettingSpec{Key: NodePathSetting, Type: "string", Default: ""})
	api := fakeapi.New(t, m, extension.ScopeInfo{Name: "tab", Folder: folder})
	api.SetTestingUnavailable(true)
	return api
}
