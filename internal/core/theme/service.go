package theme

import (
	"sync"

	"github.com/rm4n0s/errors"
)

// Settings are the settings that choose the theme (SPEC §8.4).
type Settings struct {
	// ColorTheme is workbench.colorTheme: the label or id of the theme.
	ColorTheme string
	// AutoDetectColorScheme is window.autoDetectColorScheme: follow the light or dark scheme of
	// the system with the two preferred themes.
	AutoDetectColorScheme bool
	// PreferredDarkColorTheme is workbench.preferredDarkColorTheme.
	PreferredDarkColorTheme string
	// PreferredLightColorTheme is workbench.preferredLightColorTheme.
	PreferredLightColorTheme string
}

// Service holds the theme the UI uses (SPEC §5.1, §8.4): it picks the theme from the settings and
// the brightness of the system, loads it, and tells the listeners when it changes. It is safe
// for concurrent use; listeners are called on the goroutine that made the change.
type Service struct {
	registry *ColorRegistry

	mu        sync.Mutex
	user      []UserTheme
	settings  Settings
	darkOS    bool
	current   *ColorTheme
	info      Info
	listeners map[int]func(*ColorTheme)
	nextID    int
	// bundled keeps the bundled themes that were loaded, so that choosing one again is the same
	// theme and not a change.
	bundled map[string]*Theme
}

// NewService makes a service over a color registry, with Dark Modern as the theme until the
// settings are applied.
func NewService(registry *ColorRegistry) *Service {
	s := &Service{
		registry:  registry,
		settings:  Settings{ColorTheme: DefaultDark, PreferredDarkColorTheme: DefaultDark, PreferredLightColorTheme: DefaultLight},
		darkOS:    true,
		listeners: map[int]func(*ColorTheme){},
		bundled:   map[string]*Theme{},
	}
	// The default theme is bundled, so loading it cannot fail unless the program is broken.
	if _, err := s.reselect(); err != nil {
		panic(err)
	}
	return s
}

// Registry is the color registry the service resolves colors with.
func (s *Service) Registry() *ColorRegistry { return s.registry }

// Current is the theme in use.
func (s *Service) Current() *ColorTheme {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// CurrentInfo describes the theme in use.
func (s *Service) CurrentInfo() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

// Themes lists the themes that can be chosen: the bundled ones, then the user's.
func (s *Service) Themes() ([]Info, error) {
	list, err := Bundled()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.user {
		list = append(list, u.Info)
	}
	return list, nil
}

// SetUserThemes sets the themes of the user's folder. The theme in use is chosen again, since
// the one the settings name may be among them now. See Apply for the error.
func (s *Service) SetUserThemes(themes []UserTheme) error {
	s.mu.Lock()
	s.user = append([]UserTheme(nil), themes...)
	s.mu.Unlock()
	return s.apply(func() {})
}

// Apply sets the settings that choose the theme. The theme in use is chosen again; when it
// changes, the listeners are told. When the theme the settings name does not exist or cannot be
// read, the default theme of the system's brightness is used and the error says why: it has the
// tag ThemeNotFound or the tags of Load.
func (s *Service) Apply(settings Settings) error {
	return s.apply(func() { s.settings = settings })
}

// SetPlatformBrightness tells the service whether the system is dark. It matters when
// window.autoDetectColorScheme is on.
func (s *Service) SetPlatformBrightness(dark bool) error {
	return s.apply(func() { s.darkOS = dark })
}

// apply changes the state under the lock and chooses the theme again.
func (s *Service) apply(change func()) error {
	s.mu.Lock()
	change()
	changed, err := s.reselectLocked()
	current := s.current
	var listeners []func(*ColorTheme)
	if changed {
		for _, fn := range s.listeners {
			listeners = append(listeners, fn)
		}
	}
	s.mu.Unlock()
	for _, fn := range listeners {
		fn(current)
	}
	return err
}

// reselect is reselectLocked for use before the service is shared.
func (s *Service) reselect() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reselectLocked()
}

// OnChange calls fn with the new theme whenever the theme in use changes. It gives a function
// that stops the calls.
func (s *Service) OnChange(fn func(*ColorTheme)) (unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	s.listeners[id] = fn
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.listeners, id)
	}
}

// chosenName is the name of the theme the settings and the system's brightness ask for.
func (s *Service) chosenName() string {
	if s.settings.AutoDetectColorScheme {
		if s.darkOS {
			return orDefault(s.settings.PreferredDarkColorTheme, DefaultDark)
		}
		return orDefault(s.settings.PreferredLightColorTheme, DefaultLight)
	}
	return orDefault(s.settings.ColorTheme, DefaultDark)
}

func orDefault(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// reselectLocked chooses the theme again and says whether it is another one than before.
func (s *Service) reselectLocked() (changed bool, err error) {
	name := s.chosenName()
	theme, info, err := s.find(name)
	if err != nil {
		fallback := DefaultDark
		if s.settings.AutoDetectColorScheme && !s.darkOS {
			fallback = DefaultLight
		}
		var ferr error
		if theme, info, ferr = s.find(fallback); ferr != nil {
			return false, ferr
		}
	}
	if s.current != nil && s.info == info && s.current.theme == theme {
		return false, err
	}
	s.current = NewColorTheme(theme, s.registry)
	s.info = info
	return true, err
}

// find loads the theme with a label or id, among the bundled themes and then the user's.
func (s *Service) find(name string) (*Theme, Info, error) {
	list, err := Bundled()
	if err != nil {
		return nil, Info{}, err
	}
	for _, info := range list {
		if info.Label == name || info.ID == name {
			if t, ok := s.bundled[info.Path]; ok {
				return t, info, nil
			}
			t, err := Load(assets, info.Path, info.Type)
			if err != nil {
				return nil, Info{}, err
			}
			s.bundled[info.Path] = t
			return t, info, nil
		}
	}
	for _, u := range s.user {
		if u.Label == name || u.ID == name {
			return u.Theme, u.Info, nil
		}
	}
	return nil, Info{}, errors.New("ThemeNotFound", "no theme has this name", "name", name)
}
