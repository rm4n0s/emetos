package cpp

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// The debuggers of the pack (SPEC §10.31 "The debuggers", C, C++ and Rust; step 25.9): the pack owns the debug types gdb and lldb-dap
// that extensions/internal/nativedebug implements (its tests are there); here is what the pack declares and registers.

func TestThePackDeclaresTheNativeDebuggers(t *testing.T) {
	m := New().Manifest()
	var types []string
	for _, d := range m.Contributes.Debuggers {
		types = append(types, d.Type)
	}
	if want := []string{nativedebug.TypeGDB, nativedebug.TypeLLDB, nativedebug.TypeLLDBOld}; !slices.Equal(types, want) {
		t.Errorf("debuggers = %v, want %v", types, want)
	}
	var langs []string
	for _, b := range m.Contributes.Breakpoints {
		langs = append(langs, b.Language)
	}
	if want := []string{"c", "cpp", "cuda-cpp"}; !slices.Equal(langs, want) {
		t.Errorf("breakpoint languages = %v, want %v", langs, want)
	}
	have := map[string]bool{}
	for _, s := range m.Contributes.Settings {
		have[s.Key] = true
	}
	for _, s := range nativedebug.SettingSpecs() {
		if !have[s.Key] {
			t.Errorf("the setting %s is not declared", s.Key)
		}
	}
}

func TestThePackRegistersTheAdaptersAndTheProviders(t *testing.T) {
	r := newRig(t, false, nil)
	got := r.api.DebugFactoryTypes()
	slices.Sort(got)
	if want := []string{nativedebug.TypeGDB, nativedebug.TypeLLDB, nativedebug.TypeLLDBOld}; !slices.Equal(got, want) {
		t.Errorf("factories = %v, want %v", got, want)
	}
	list, err := r.api.ProvideDebugConfigurations(context.Background(), nativedebug.TypeGDB, r.folder, extension.DebugTriggerDynamic)
	if err != nil || len(list) != 1 || list[0]["name"] != nativedebug.DynamicName {
		t.Errorf("dynamic configurations = %v, %v", list, err)
	}
}

func TestAGdbThatIsNotThereIsToldWhenAnAdapterIsAsked(t *testing.T) {
	r := newRig(t, false, nil)
	s := r.api.NewDebugSession("s1", nativedebug.TypeGDB, "g", extension.DebugConfiguration{"request": "launch"})
	if _, err := r.api.DebugAdapter(context.Background(), nativedebug.TypeGDB, s, nil); err == nil {
		t.Fatal("no error")
	}
	if note := r.note(); note == "" {
		t.Error("the user is not told")
	}
}
