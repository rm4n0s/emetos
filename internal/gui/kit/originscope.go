package kit

import "github.com/rm4n0s/glutter/widgets"

// OriginScope is the inherited widget that says which screen of a hosted workspace its subtree is on (SPEC §10.18 "As built (step
// 24.25)"): 0 for the window of the host, or the number of the view of a client. The widgets of an extension's view read it with OriginOf, so
// that a terminal drawn in two views keeps what each of them shows and has the keyboard for.
type OriginScope struct {
	widgets.InheritedWidget
	origin uint64
}

// NewOriginScope makes a scope for a screen.
func NewOriginScope(origin uint64, child widgets.Widget) *OriginScope {
	return &OriginScope{InheritedWidget: widgets.NewInheritedWidgetBase(child), origin: origin}
}

// UpdateShouldNotify implements widgets.InheritedWidgetInterface.
func (s *OriginScope) UpdateShouldNotify(old widgets.Widget) bool {
	return s.origin != old.(*OriginScope).origin
}

// OriginOf is the origin of the screen above bc: 0, the host's window, when there is no scope.
func OriginOf(bc widgets.BuildContext) uint64 {
	if s, ok := widgets.GetInherited[*OriginScope](bc); ok {
		return s.origin
	}
	return 0
}
