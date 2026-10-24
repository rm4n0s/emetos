package editor

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

func TestApplySaveParticipants(t *testing.T) {
	all := edit.SaveParticipants{TrimTrailingWhitespace: true, InsertFinalNewline: true, TrimFinalNewlines: true}
	tests := []struct {
		name     string
		text     string
		sels     []buffer.Selection
		p        edit.SaveParticipants
		autoSave bool
		want     string
		wantSels []buffer.Selection
	}{
		{"nothing on", "a  ", []buffer.Selection{cur(0, 3)}, edit.SaveParticipants{}, false, "a  ", []buffer.Selection{cur(0, 3)}},
		{"all on, nothing to do", "a\n", []buffer.Selection{cur(0, 1)}, all, false, "a\n", []buffer.Selection{cur(0, 1)}},
		{"all on", "a  \nb \n\n\n", []buffer.Selection{cur(1, 1)}, all, false, "a\nb\n", []buffer.Selection{cur(1, 1)}},
		{"the cursor in trimmed space goes to the end of the line", "ab   ", []buffer.Selection{cur(0, 5)}, edit.SaveParticipants{TrimTrailingWhitespace: true}, false, "ab", []buffer.Selection{cur(0, 2)}},
		{"an automatic save keeps the space before the cursor", "ab   ", []buffer.Selection{cur(0, 4)}, edit.SaveParticipants{TrimTrailingWhitespace: true}, true, "ab  ", []buffer.Selection{cur(0, 4)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := NewViewModel(newTestDoc(tt.text), testOptions(), nil)
			vm.Cursors().SetSelections(tt.sels)
			if err := vm.ApplySaveParticipants(tt.p, tt.autoSave); err != nil {
				t.Fatal(err)
			}
			if got := vm.Document().Text(); got != tt.want {
				t.Errorf("text %q, want %q", got, tt.want)
			}
			got := vm.Cursors().Selections()
			if len(got) != len(tt.wantSels) || got[0] != tt.wantSels[0] {
				t.Errorf("selections %v, want %v", got, tt.wantSels)
			}
		})
	}
}

// The edits of a save are an undo step of their own, apart from the typing before and after.
func TestSaveParticipantsAreTheirOwnUndoStep(t *testing.T) {
	vm := NewViewModel(newTestDoc("a  "), testOptions(), nil)
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 3)})
	if err := vm.RunCommand("type", map[string]any{"text": "b"}); err != nil {
		t.Fatal(err)
	}
	if err := vm.ApplySaveParticipants(edit.SaveParticipants{TrimTrailingWhitespace: true}, false); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "a  b" {
		// the space is before the typed character: nothing is trimmed
		t.Fatalf("text %q", got)
	}
	if err := vm.RunCommand("deleteLeft", nil); err != nil {
		t.Fatal(err)
	}
	if err := vm.ApplySaveParticipants(edit.SaveParticipants{TrimTrailingWhitespace: true}, false); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "a" {
		t.Fatalf("text %q", got)
	}
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "a  " {
		t.Errorf("after one undo %q, want only the trimming undone", got)
	}
}
