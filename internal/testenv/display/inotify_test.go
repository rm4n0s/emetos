package display

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/app"
)

// fakeDev answers dev.toasts with a list and records the ids dev.dismissNotification gets.
type fakeDev struct {
	toasts    []app.DevToast
	dismissed []uint64
}

func (f *fakeDev) Request(command string, args any) (app.DevResponse, error) {
	switch command {
	case "dev.toasts":
		raw, err := json.Marshal(f.toasts)
		return app.DevResponse{OK: true, Result: raw}, err
	case "dev.dismissNotification":
		f.dismissed = append(f.dismissed, args.(uint64))
	}
	return app.DevResponse{OK: true}, nil
}

func TestDismissWatchWarning(t *testing.T) {
	for _, tt := range []struct {
		name          string
		toasts        []app.DevToast
		wantDismissed bool
		wantIDs       []uint64
	}{
		{"no toast", nil, false, nil},
		{"only other toasts", []app.DevToast{{ID: 1, Message: "Build finished"}, {ID: 2, Message: "Cannot watch"}}, false, nil},
		{"the warning among others", []app.DevToast{
			{ID: 3, Message: "Build finished"},
			{ID: 2, Severity: "warning", Source: "Emetos", Message: watchWarningPrefix + ", so it checks for them every few seconds instead."},
		}, true, []uint64{2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeDev{toasts: tt.toasts}
			if got := dismissWatchWarning(f, 100*time.Millisecond); got != tt.wantDismissed {
				t.Errorf("dismissed = %v, want %v", got, tt.wantDismissed)
			}
			if len(f.dismissed) != len(tt.wantIDs) || (len(tt.wantIDs) == 1 && f.dismissed[0] != tt.wantIDs[0]) {
				t.Errorf("dismissed ids = %v, want %v", f.dismissed, tt.wantIDs)
			}
		})
	}
}

func TestInotifyAvailableDoesNotLeakInstances(t *testing.T) {
	// the probe closes what it opens: many probes do not use the instances up
	for range 300 {
		inotifyAvailable()
	}
}
