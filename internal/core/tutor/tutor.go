// Package tutor makes the practice copies of the Emetos Tutor (SPEC §10.35): a folder of lessons, in the spirit of
// vimtutor, that the user edits in the editor itself. It picks the folder of a copy, writes the lessons into it with
// the user's own keys in place of the placeholders, and tells a practice folder from the others. It never imports
// the UI.
package tutor

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// MainFile is the lesson file a copy opens with.
const MainFile = "TUTOR.md"

// maxCopies bounds the folders tried for a copy: tutor, tutor-2 ... tutor-<maxCopies>.
const maxCopies = 100

// placeholder is {{key:<command id>}} in a lesson.
var placeholder = regexp.MustCompile(`\{\{key:([A-Za-z0-9_.\-]+)\}\}`)

// practiceName is the name of a practice folder: tutor or tutor-<n>.
var practiceName = regexp.MustCompile(`^tutor(-[0-9]+)?$`)

// KeyLabeler tells how a command is run: the label of its key, and its title for a command with none.
type KeyLabeler interface {
	// KeyLabel is the label of the key that runs a command ("Ctrl+D"), or "" when it has no key.
	KeyLabel(command string) string
	// CommandTitle is the title of a command as the Command Palette shows it, and whether the command exists.
	CommandTitle(command string) (string, bool)
}

// Base is the folder that holds the practice copies: $XDG_CACHE_HOME/emetos.
func Base(cacheHome string) string { return filepath.Join(cacheHome, "emetos") }

// Folder picks the folder of a new copy: tutor, or tutor-2, tutor-3 ... when inUse says a workspace still has the
// earlier ones open. The error is TutorNoFreeFolder.
func Folder(cacheHome string, inUse func(dir string) bool) (string, error) {
	base := Base(cacheHome)
	for n := 1; n <= maxCopies; n++ {
		name := "tutor"
		if n > 1 {
			name += "-" + strconv.Itoa(n)
		}
		dir := filepath.Join(base, name)
		if !inUse(dir) {
			return dir, nil
		}
	}
	return "", errors.New("TutorNoFreeFolder", "every practice folder of the tutor is open", "base", base, "tried", maxCopies)
}

// IsPractice tells whether a path is a practice folder of the tutor or inside one, so that it is kept out of the
// recently opened folders and files.
func IsPractice(cacheHome, p string) bool {
	rel, err := filepath.Rel(Base(cacheHome), p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return practiceName.MatchString(first)
}

// Placeholders lists the commands that the lessons under root name with {{key:...}}, each once, sorted.
func Placeholders(lessons fs.FS, root string) ([]string, error) {
	seen := map[string]bool{}
	err := fs.WalkDir(lessons, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".md" {
			return err
		}
		b, err := fs.ReadFile(lessons, p)
		if err != nil {
			return err
		}
		for _, m := range placeholder.FindAllSubmatch(b, -1) {
			seen[string(m[1])] = true
		}
		return nil
	})
	if err != nil {
		return nil, errors.NewErr("TutorLessonsUnreadable", err).SetMetadata("root", root)
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// Expansions gives the text that replaces the placeholder of each command: the label of its key, or how to run it
// from the Command Palette when it has none.
func Expansions(commands []string, keys KeyLabeler) map[string]string {
	out := make(map[string]string, len(commands))
	for _, id := range commands {
		if label := keys.KeyLabel(id); label != "" {
			out[id] = label
			continue
		}
		title, ok := keys.CommandTitle(id)
		if !ok || title == "" {
			title = id
		}
		out[id] = `(no key: run "` + title + `" from the Command Palette)`
	}
	return out
}

// Copy writes a fresh practice copy into dir: the folder is emptied (it must be a practice folder of Base) and the
// files under root of lessons are written into it, with the placeholders of the Markdown files replaced by
// expansions. The error is TutorCopyFailed, with the folder in its metadata.
func Copy(ctx context.Context, lessons fs.FS, root, dir string, expansions map[string]string) error {
	if !practiceName.MatchString(filepath.Base(dir)) {
		return errors.New("TutorCopyFailed", "the folder is not a practice folder of the tutor", "folder", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		return errors.NewErr("TutorCopyFailed", err).SetMetadata("folder", dir)
	}
	err := fs.WalkDir(lessons, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(lessons, p)
		if err != nil {
			return err
		}
		if path.Ext(p) == ".md" {
			b = placeholder.ReplaceAllFunc(b, func(m []byte) []byte {
				id := string(placeholder.FindSubmatch(m)[1])
				if text, ok := expansions[id]; ok {
					return []byte(text)
				}
				return []byte(id)
			})
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		return errors.NewErr("TutorCopyFailed", err).SetMetadata("folder", dir)
	}
	return nil
}
