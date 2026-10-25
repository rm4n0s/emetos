package storage

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// DefaultProfileName is the name the default profile is shown with and the name that selects it (SPEC §10.12); its
// folders are the plain emetos/ ones and its identifier in Paths is DefaultProfile (the empty string). No other
// profile can have this name, in any capitalization.
const DefaultProfileName = "Default"

// MaxProfileNameLength is the most characters a profile name has (SPEC §10.12).
const MaxProfileNameLength = 64

// ProfileDisplayName is the name a profile is shown with: DefaultProfileName for the default profile.
func ProfileDisplayName(profile string) string {
	if profile == DefaultProfile {
		return DefaultProfileName
	}
	return profile
}

// ValidProfileName tells whether name follows the rules for the names of profiles (SPEC §10.12): 1 to 64
// characters, each a letter, a digit, a space, '-' or '_', with no space at the start or the end. It is the single
// source of the rule: the command line uses it for --profile. It does not look at the profiles that exist (a name
// that is taken or reserved is the business of Profiles.Create and Profiles.Rename).
func ValidProfileName(name string) bool {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > MaxProfileNameLength {
		return false
	}
	if strings.HasPrefix(name, " ") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if !profileNameRune(r) {
			return false
		}
	}
	return true
}

// profileNameRune tells whether a profile name may hold the character: a letter or digit (Unicode ones too), a
// space, '-' or '_'.
func profileNameRune(r rune) bool {
	switch {
	case r == ' ', r == '-', r == '_':
		return true
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return true
	case r > unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
		return true
	}
	return false
}

// ValidateName is ValidProfileName as an error, with the tag ProfileNameInvalid.
func ValidateName(name string) error {
	if !ValidProfileName(name) {
		return errors.New("ProfileNameInvalid",
			"a profile name has 1 to 64 letters, digits, spaces, - or _, and no space at its start or end", "name", name)
	}
	return nil
}

// isDefaultName tells whether a name selects the default profile: the empty string and "Default" in any
// capitalization.
func isDefaultName(name string) bool {
	return name == DefaultProfile || strings.EqualFold(name, DefaultProfileName)
}

// profileFiles are the files of the configuration folder of a profile that belong to the profile and are
// copied when a profile is made from another one (SPEC §10.12): the settings, the keybindings and the snippets
// folder. The user's color themes are shared by all the profiles (Paths.ThemesDir); the state and the backups
// are not in the configuration folder, and are never copied.
var (
	profileFiles = []string{"settings.json", "keybindings.json"}
	profileDirs  = []string{"snippets"}
)

// CreateSource says what a new profile starts with.
type CreateSource struct {
	// From is the profile whose settings, keybindings and snippets are copied; ignored when Copy is false.
	From string
	// Copy says that the new profile is made from the profile From. Otherwise it is empty.
	Copy bool
}

// CreateEmpty is the source of a profile that starts empty.
var CreateEmpty = CreateSource{}

// CreateFrom is the source of a profile that starts as a copy of another one (the default profile is "" or
// "Default"). "From the current profile" is CreateFrom with the profile of the window.
func CreateFrom(profile string) CreateSource { return CreateSource{From: profile, Copy: true} }

// ProfilesOptions say what a Profiles is made of.
type ProfilesOptions struct {
	// State is the state store of the default profile, which keeps the profile of each folder and of each
	// window (GlobalState.LastProfiles, WorkspaceTab.Profile). Nil keeps none of them.
	State *StateStore
	// Logger gets the failures of the bookkeeping that does not fail an operation; slog.Default() when nil.
	Logger *slog.Logger
	// CopyFile copies a file; the copy of the bytes when nil. Tests give one that fails.
	CopyFile func(src, dst string) error
	// Rename moves a folder; os.Rename when nil. Tests give one that fails.
	Rename func(oldPath, newPath string) error
	// WriteFile writes a file of an imported profile; os.WriteFile when nil. Tests give one that fails.
	WriteFile func(path string, data []byte, perm os.FileMode) error
	// RemoveAll removes a folder with what is in it; os.RemoveAll when nil. Tests give one that fails.
	RemoveAll func(path string) error
}

// Profiles manages the profiles (SPEC §10.12): their list, their creation (empty or from another one), renaming
// and deletion, and the profile last used with each folder. A profile is a folder in the configuration folder
// (Paths.ConfigDir) and, once it has been used, one in the state folder (Paths.StateDir). Every method that takes a
// profile takes its name as the user types it: "" and "Default" in any capitalization are the default profile, and
// another name is matched without regard to its capitalization. The methods return the name as it is stored. It is
// safe for concurrent use.
type Profiles struct {
	paths Paths
	opts  ProfilesOptions

	// mu orders the operations that change the folders.
	mu sync.Mutex
}

// NewProfiles makes the manager of the profiles of paths. It reads and writes nothing yet.
func NewProfiles(paths Paths, opts ProfilesOptions) *Profiles {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.CopyFile == nil {
		opts.CopyFile = copyFile
	}
	if opts.Rename == nil {
		opts.Rename = os.Rename
	}
	if opts.RemoveAll == nil {
		opts.RemoveAll = os.RemoveAll
	}
	if opts.WriteFile == nil {
		opts.WriteFile = os.WriteFile
	}
	return &Profiles{paths: paths, opts: opts}
}

// root is the folder that holds the configuration folders of the profiles.
func (p *Profiles) root() string {
	return filepath.Join(p.paths.ConfigHome, appDir, "profiles")
}

// List gives the profiles: the default profile first (as DefaultProfile, the empty string), then the others
// sorted by name without regard to capitalization. A folder whose name is not a valid profile name is not a
// profile. The error has the tag ProfileListFailed.
func (p *Profiles) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("ProfileListFailed", err)
	}
	entries, err := os.ReadDir(p.root())
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.NewErr("ProfileListFailed", err).SetMetadata("path", p.root())
	}
	out := []string{DefaultProfile}
	var named []string
	for _, e := range entries {
		if !e.IsDir() || !ValidProfileName(e.Name()) || isDefaultName(e.Name()) {
			continue
		}
		named = append(named, e.Name())
	}
	slices.SortFunc(named, func(a, b string) int {
		if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return append(out, named...), nil
}

// Lookup finds a profile by the name the user gave: "" and "Default" in any capitalization are the default
// profile, another name is matched without regard to capitalization. It gives the profile as it is stored (the
// default profile as DefaultProfile) and false when there is no such profile. The error has the tag of List.
func (p *Profiles) Lookup(ctx context.Context, name string) (profile string, ok bool, err error) {
	if isDefaultName(name) {
		return DefaultProfile, true, nil
	}
	list, err := p.List(ctx)
	if err != nil {
		return "", false, err
	}
	for _, have := range list[1:] {
		if strings.EqualFold(have, name) {
			return have, true, nil
		}
	}
	return "", false, nil
}

// Exists tells whether the profile exists; an error of List counts as no.
func (p *Profiles) Exists(ctx context.Context, name string) bool {
	_, ok, err := p.Lookup(ctx, name)
	return err == nil && ok
}

// Path is the configuration folder of the profile, which is there only for a profile that exists. The error has
// the tag ProfilePathUnsafe.
func (p *Profiles) Path(name string) (string, error) {
	if isDefaultName(name) {
		return p.paths.ConfigDir(DefaultProfile)
	}
	return p.paths.ConfigDir(name)
}

// resolve finds an existing profile or fails with the tag ProfileNotFound.
func (p *Profiles) resolve(ctx context.Context, name string) (string, error) {
	profile, ok, err := p.Lookup(ctx, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("ProfileNotFound", "there is no such profile", "name", name)
	}
	return profile, nil
}

// checkNewName checks a name for a profile that is made or renamed to: valid (ProfileNameInvalid), not the name
// of the default profile (ProfileNameReserved) and not the name of another profile, without regard to
// capitalization (ProfileExists). except is a profile that may keep its own name with another capitalization.
func (p *Profiles) checkNewName(ctx context.Context, name, except string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if isDefaultName(name) {
		return errors.New("ProfileNameReserved", "the name Default belongs to the default profile", "name", name)
	}
	list, err := p.List(ctx)
	if err != nil {
		return err
	}
	for _, have := range list[1:] {
		if strings.EqualFold(have, name) && have != except {
			return errors.New("ProfileExists", "a profile with that name exists already", "name", name, "existing", have)
		}
	}
	return nil
}

// CheckName checks a name for a profile that is made or renamed to, as Create and Rename do before they change
// anything: the error has the tags ProfileNameInvalid, ProfileNameReserved and ProfileExists (a name that differs from
// another profile's only in capitalization clashes with it) and those of List. except is a profile that may keep its
// own name, in another capitalization (the profile that is renamed); "" for none. The commands use it to tell the
// user what is wrong while they type a name.
func (p *Profiles) CheckName(ctx context.Context, name, except string) error {
	return p.checkNewName(ctx, name, except)
}

// Create makes a profile and gives its name. It is empty or a copy of the settings, the keybindings and the
// snippets of another profile; nothing else is copied (not the themes, which are shared, and not the state or the
// backups). The folder is made whole or not at all: it is built under a temporary name beside the others and
// renamed into place, so a failure leaves no half-made profile. The error has the tags ProfileNameInvalid,
// ProfileNameReserved, ProfileExists, ProfileNotFound (the source), ProfileCopyFailed and ProfileCreateFailed.
func (p *Profiles) Create(ctx context.Context, name string, from CreateSource) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.create(ctx, name, from)
}

func (p *Profiles) create(ctx context.Context, name string, from CreateSource) (string, error) {
	if err := p.checkNewName(ctx, name, ""); err != nil {
		return "", err
	}
	var source string
	if from.Copy {
		profile, err := p.resolve(ctx, from.From)
		if err != nil {
			return "", err
		}
		if source, err = p.paths.ConfigDir(profile); err != nil {
			return "", err
		}
	}
	dir, err := p.paths.ConfigDir(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p.root(), 0o755); err != nil {
		return "", errors.NewErr("ProfileCreateFailed", err).SetMetadata("path", p.root())
	}
	tmp, err := os.MkdirTemp(p.root(), ".new-")
	if err != nil {
		return "", errors.NewErr("ProfileCreateFailed", err).SetMetadata("path", p.root())
	}
	done := false
	defer p.removeUnfinished(tmp, &done)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return "", errors.NewErr("ProfileCreateFailed", err).SetMetadata("path", tmp)
	}
	if from.Copy {
		if err := p.copyProfile(ctx, source, tmp); err != nil {
			return "", err
		}
	}
	if err := p.opts.Rename(tmp, dir); err != nil {
		return "", errors.NewErr("ProfileCreateFailed", err).SetMetadata("path", dir)
	}
	done = true
	return name, nil
}

// removeUnfinished removes the temporary folder of a profile that was not made whole (done is false), so that a
// failure leaves nothing; Create and Import defer it. p.mu is held.
func (p *Profiles) removeUnfinished(tmp string, done *bool) {
	if *done {
		return
	}
	if err := p.opts.RemoveAll(tmp); err != nil {
		p.opts.Logger.Warn("a profile that was not made could not be removed", "path", tmp, "err", err)
	}
}

// Ensure gives the profile of a name, making it empty when there is none (what --profile does with a name that is
// not known, as VS Code does). created tells that it was made. The error has the tags of Lookup and Create.
func (p *Profiles) Ensure(ctx context.Context, name string) (profile string, created bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	profile, ok, err := p.Lookup(ctx, name)
	if err != nil || ok {
		return profile, false, err
	}
	profile, err = p.create(ctx, name, CreateEmpty)
	return profile, err == nil, err
}

// copyProfile copies the files of the configuration folder src that belong to a profile into dst (a folder that
// exists). What is not a regular file or a folder (a link, a socket) is left out. The error has the tag
// ProfileCopyFailed.
func (p *Profiles) copyProfile(ctx context.Context, src, dst string) error {
	for _, name := range profileFiles {
		if err := p.copyEntry(ctx, filepath.Join(src, name), filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	for _, name := range profileDirs {
		if err := p.copyEntry(ctx, filepath.Join(src, name), filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	return nil
}

// copyEntry copies a regular file or a folder with its regular files; an entry that is not there is skipped.
func (p *Profiles) copyEntry(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("ProfileCopyFailed", err).SetMetadata("path", src)
	}
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.NewErr("ProfileCopyFailed", err).SetMetadata("path", src)
	}
	switch {
	case info.Mode().IsRegular():
		if err := p.opts.CopyFile(src, dst); err != nil {
			return errors.NewErr("ProfileCopyFailed", err).SetMetadata("path", src)
		}
	case info.IsDir():
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return errors.NewErr("ProfileCopyFailed", err).SetMetadata("path", dst)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return errors.NewErr("ProfileCopyFailed", err).SetMetadata("path", src)
		}
		for _, e := range entries {
			if err := p.copyEntry(ctx, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile copies the bytes of a regular file into a new file with the permissions 0644.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// Rename gives a profile another name, keeping its files and its state; the profiles of folders and windows that
// had the old name have the new one. The default profile cannot be renamed (ProfileDefaultImmutable). A change of
// capitalization is a rename. The error has the tags ProfileDefaultImmutable, ProfileNotFound,
// ProfileNameInvalid, ProfileNameReserved, ProfileExists and ProfileRenameFailed. Whether a window uses the
// profile is for the caller to check: the folder moves under it.
func (p *Profiles) Rename(ctx context.Context, name, newName string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if isDefaultName(name) {
		return errors.New("ProfileDefaultImmutable", "the default profile cannot be renamed")
	}
	profile, err := p.resolve(ctx, name)
	if err != nil {
		return err
	}
	if err := p.checkNewName(ctx, newName, profile); err != nil {
		return err
	}
	if profile == newName {
		return nil
	}
	oldConfig, err := p.paths.ConfigDir(profile)
	if err != nil {
		return err
	}
	newConfig, err := p.paths.ConfigDir(newName)
	if err != nil {
		return err
	}
	if err := p.opts.Rename(oldConfig, newConfig); err != nil {
		return errors.NewErr("ProfileRenameFailed", err).SetMetadata("from", oldConfig, "to", newConfig)
	}
	oldState, err := p.paths.StateDir(profile)
	if err != nil {
		return err
	}
	newState, err := p.paths.StateDir(newName)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(oldState); statErr == nil {
		if err := p.opts.Rename(oldState, newState); err != nil {
			// the profile keeps its name: the configuration goes back
			if back := p.opts.Rename(newConfig, oldConfig); back != nil {
				p.opts.Logger.Error("a profile could not be renamed back", "from", newConfig, "to", oldConfig, "err", back)
			}
			return errors.NewErr("ProfileRenameFailed", err).SetMetadata("from", oldState, "to", newState)
		}
	}
	p.rewriteReferences(ctx, func(old string) string {
		if old == profile {
			return newName
		}
		return old
	})
	return nil
}

// Delete removes a profile: its configuration folder and its state folder. The default profile cannot be
// deleted (ProfileDefaultImmutable). The folders and windows that had the profile have the default profile
// again. Whether a window uses the profile is for the caller to check: the files go whatever uses them. The
// error has the tags ProfileDefaultImmutable, ProfileNotFound and ProfileDeleteFailed.
func (p *Profiles) Delete(ctx context.Context, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if isDefaultName(name) {
		return errors.New("ProfileDefaultImmutable", "the default profile cannot be deleted")
	}
	profile, err := p.resolve(ctx, name)
	if err != nil {
		return err
	}
	for _, dirOf := range []func(string) (string, error){p.paths.ConfigDir, p.paths.StateDir} {
		dir, err := dirOf(profile)
		if err != nil {
			return err
		}
		if err := p.opts.RemoveAll(dir); err != nil {
			return errors.NewErr("ProfileDeleteFailed", err).SetMetadata("path", dir)
		}
	}
	p.rewriteReferences(ctx, func(old string) string {
		if old == profile {
			return DefaultProfile
		}
		return old
	})
	return nil
}

// rewriteReferences maps the profile of every folder and window the global state records. A failure is logged:
// the folders are changed already.
func (p *Profiles) rewriteReferences(ctx context.Context, mapTo func(string) string) {
	s := p.opts.State
	if s == nil {
		return
	}
	err := s.UpdateGlobal(ctx, func(g *GlobalState) bool {
		changed := false
		for folder, profile := range g.LastProfiles {
			switch next := mapTo(profile); {
			case next == DefaultProfile:
				delete(g.LastProfiles, folder)
				changed = true
			case next != profile:
				g.LastProfiles[folder] = next
				changed = true
			}
		}
		for i := range g.WorkspaceTabs {
			if next := mapTo(g.WorkspaceTabs[i].Profile); next != g.WorkspaceTabs[i].Profile {
				g.WorkspaceTabs[i].Profile = next
				changed = true
			}
		}
		return changed
	})
	if err == nil {
		err = s.Flush(context.WithoutCancel(ctx))
	}
	if err != nil {
		p.opts.Logger.Warn("the profiles of the folders could not be updated", "err", err)
	}
}

// LastProfile is the profile last used with a folder, when it still exists. A folder with no record, and one
// whose profile is gone, have none (false: the window uses the default profile). The error has the tags of List.
func (p *Profiles) LastProfile(ctx context.Context, folder string) (profile string, ok bool, err error) {
	if p.opts.State == nil || !filepath.IsAbs(folder) {
		return DefaultProfile, false, nil
	}
	g, loadErr := p.opts.State.LoadGlobal(ctx)
	if loadErr != nil {
		// an unusable state file is an empty state: the folder has no record
		p.opts.Logger.Warn("the global UI state cannot be used", "err", loadErr)
	}
	name, has := g.LastProfiles[filepath.Clean(folder)]
	if !has {
		return DefaultProfile, false, nil
	}
	profile, ok, err = p.Lookup(ctx, name)
	if err != nil || !ok {
		return DefaultProfile, false, err
	}
	return profile, true, nil
}

// SetLastProfile notes the profile a folder is used with; the default profile removes the record (a folder with
// none uses the default profile). The change is written after the delay of the state store. The error has the
// tags ProfileFolderInvalid (the folder is not an absolute path), ProfileNotFound and those of
// StateStore.UpdateGlobal.
func (p *Profiles) SetLastProfile(ctx context.Context, folder, name string) error {
	if !filepath.IsAbs(folder) {
		return errors.New("ProfileFolderInvalid", "the folder of a profile record is an absolute path", "folder", folder)
	}
	profile, err := p.resolve(ctx, name)
	if err != nil {
		return err
	}
	if p.opts.State == nil {
		return nil
	}
	folder = filepath.Clean(folder)
	return p.opts.State.UpdateGlobal(ctx, func(g *GlobalState) bool {
		have, has := g.LastProfiles[folder]
		if profile == DefaultProfile {
			delete(g.LastProfiles, folder)
			return has
		}
		if has && have == profile {
			return false
		}
		if g.LastProfiles == nil {
			g.LastProfiles = map[string]string{}
		}
		g.LastProfiles[folder] = profile
		return true
	})
}
