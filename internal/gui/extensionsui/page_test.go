package extensionsui

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

func TestStatusText(t *testing.T) {
	failed := &exthost.Failure{BoxText: "Tools failed"}
	tests := []struct {
		name    string
		row     exthost.Row
		want    string
		failure bool
	}{
		{"no tabs", exthost.Row{}, "Not active yet.", false},
		{"inactive in a tab", exthost.Row{Scopes: []exthost.ScopeRow{{Scope: "a", State: exthost.StateInactive}}}, "Not active yet.", false},
		{"active in two tabs, in order", exthost.Row{Scopes: []exthost.ScopeRow{
			{Scope: "b", State: exthost.StateActive}, {Scope: "a", State: exthost.StateActive}, {Scope: "c", State: exthost.StateInactive},
		}}, "Active in a, b.", false},
		{"disabled wins over the tabs", exthost.Row{Disabled: true, Scopes: []exthost.ScopeRow{{Scope: "a", State: exthost.StateActive}}}, "Disabled", false},
		{"failed in one tab", exthost.Row{Scopes: []exthost.ScopeRow{{Scope: "a", State: exthost.StateFailed, Failure: failed}}}, "Failed in a: Tools failed.", true},
		{"failed in one tab and active in another", exthost.Row{Scopes: []exthost.ScopeRow{
			{Scope: "a", State: exthost.StateFailed, Failure: failed}, {Scope: "b", State: exthost.StateActive},
		}}, "Failed in a: Tools failed. Active in b.", true},
		{"a failure without a record", exthost.Row{Scopes: []exthost.ScopeRow{{Scope: "a", State: exthost.StateFailed}}}, "Failed in a.", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, failure := StatusText(tt.row)
			if got != tt.want || failure != tt.failure {
				t.Errorf("StatusText = %q, %v; want %q, %v", got, failure, tt.want, tt.failure)
			}
		})
	}
}
