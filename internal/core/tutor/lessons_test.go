package tutor

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/assets"
)

// The lessons of the Emetos Tutor (SPEC §10.35 "The lessons"). That every {{key:...}} is a command with a default key
// is checked in cmd/emetos, which sees the keys of the extensions too.

// lessonsText is TUTOR.md of the embedded lessons.
func lessonsText(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(assets.Tutor(), assets.TutorRoot+"/"+MainFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTheLessonsAreElevenInOrder(t *testing.T) {
	heading := regexp.MustCompile(`(?m)^## Lesson (\d+): (.+)$`)
	found := heading.FindAllStringSubmatch(lessonsText(t), -1)
	if len(found) != 11 {
		t.Fatalf("%d lessons, want 11", len(found))
	}
	for i, m := range found {
		if m[1] != fmt.Sprint(i+1) {
			t.Errorf("lesson %d is numbered %s", i+1, m[1])
		}
	}
}

// TestEveryExerciseHasItsAnswer checks that each block of "--->" lines is followed by a block of "ok>" lines.
func TestEveryExerciseHasItsAnswer(t *testing.T) {
	lines := strings.Split(lessonsText(t), "\n")
	exercises := 0
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "---> ") {
			continue
		}
		exercises++
		start := i
		for i < len(lines) && strings.HasPrefix(lines[i], "---> ") {
			i++
		}
		if i >= len(lines) || !strings.HasPrefix(lines[i], "ok>  ") {
			t.Errorf("the exercise at line %d has no ok> line after it", start+1)
		}
		for i < len(lines) && strings.HasPrefix(lines[i], "ok>  ") {
			i++
		}
	}
	// lessons 1 to 7 edit text: nine exercises among them
	if exercises < 9 {
		t.Errorf("%d exercises", exercises)
	}
}

func TestThePracticeFilesTheLessonsNameExist(t *testing.T) {
	named := regexp.MustCompile("`(practice/[^`]+)`").FindAllStringSubmatch(lessonsText(t), -1)
	if len(named) == 0 {
		t.Fatal("the lessons name no practice file")
	}
	for _, m := range named {
		if _, err := fs.Stat(assets.Tutor(), assets.TutorRoot+"/"+m[1]); err != nil {
			t.Errorf("the lessons name %s: %v", m[1], err)
		}
	}
}

func TestTheLessonsHaveTheMainFile(t *testing.T) {
	if _, err := fs.Stat(assets.Tutor(), assets.TutorRoot+"/"+MainFile); err != nil {
		t.Fatal(err)
	}
}
