package app

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

func newTester(t *testing.T, ctx context.Context) *gluttertest.Tester {
	t.Helper()
	var loaded []gluttertest.Font
	for _, src := range fonts.Sources() {
		loaded = append(loaded, gluttertest.Font{Family: src.Family, Data: src.Data})
	}
	ts := gluttertest.New(t, gluttertest.Opts{Size: ui.Size{Width: windowWidth, Height: windowHeight}, Fonts: loaded})
	ts.PumpWidget(newApp(ctx))
	return ts
}

func TestHomeShowsTheIconAndTheName(t *testing.T) {
	ts := newTester(t, context.Background())

	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("Emetos")); err != nil {
		t.Error(err)
	}
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconCode))); err != nil {
		t.Error(err)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
	if n := ts.ExitRequests(); n != 0 {
		t.Errorf("the app asked to exit %d times without being told to", n)
	}
}

func TestCancelingTheContextExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts := newTester(t, ctx)

	cancel()
	// The goroutine that watches the context hands the exit to the UI thread.
	for range 400 {
		ts.Pump(10 * time.Millisecond)
		if ts.ExitRequests() > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if n := ts.ExitRequests(); n != 1 {
		t.Errorf("the app asked to exit %d times, want 1", n)
	}
}

func TestRunWithACanceledContextOpensNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, Options{}); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
}
