package exthost

import "slices"

// The language packs (SPEC §10.15 "As built (step 11.4)"). An extension that declares Contributes.Languages gives the
// process-wide language registry languages, grammars and settings defaults. The registry belongs to the process while the
// extensions belong to window tabs, so the host keeps count: the languages of an extension are in the registry while at
// least one scope has the extension enabled, and go with the last scope that lets go of it (the tab closes, or the user
// turns the extension off in it). Before the first scope exists every pack is registered, so the registry knows every
// language before the first file of the first tab opens; SyncLanguages then makes the registry follow extensions.disabled.

// langPack is the language pack of one extension.
type langPack struct {
	info *Info
	// registered tells whether the languages are in the registry.
	registered bool
	// holds counts the scopes that have the extension enabled.
	holds int
	// defaults are the settings defaults the pack gave, by language id and key, to take back when it goes.
	defaults map[string][]string
}

// registerLanguages adds the pack to the registry and its defaults to the settings. The lock is held. A pack whose
// package.json cannot be read is logged and stays out: the other packs are unaffected.
func (h *Host) registerLanguages(p *langPack) {
	if p.registered {
		return
	}
	id := p.info.Manifest.ID
	m, err := h.languages.AddNamedExtension(id, p.info.Manifest.Contributes.Languages)
	if err != nil {
		h.log.Warn("the languages of an extension are left out", "extension", id, "err", err)
		return
	}
	p.registered = true
	p.defaults = map[string][]string{}
	for lang, values := range m.ConfigurationDefaults {
		if h.registry == nil {
			break
		}
		if err := h.registry.RegisterLanguageDefaults(lang, values); err != nil {
			h.log.Warn("the settings of a language are left out", "extension", id, "language", lang, "err", err)
			continue
		}
		for k := range values {
			p.defaults[lang] = append(p.defaults[lang], k)
		}
	}
	for _, problem := range m.Problems {
		h.log.Warn("language problem", "extension", id, "err", problem)
	}
}

// unregisterLanguages takes the pack out of the registry, with its defaults. The lock is held.
func (h *Host) unregisterLanguages(p *langPack) {
	if !p.registered {
		return
	}
	h.languages.RemoveExtension(p.info.Manifest.ID)
	if h.registry != nil {
		for lang, keys := range p.defaults {
			h.registry.UnregisterLanguageDefaults(lang, keys)
		}
	}
	p.registered, p.defaults = false, nil
}

// SyncLanguages makes the language registry follow extensions.disabled (the ids): a pack that is not disabled is
// registered, and one that is disabled is taken out unless a scope has it enabled. The window calls it when it is made,
// before it opens any file, so a document is never given the language of a pack that is off; a scope calls it when it
// starts. It does nothing without a language registry.
func (h *Host) SyncLanguages(disabled []string) {
	if h.languages == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.packs {
		switch {
		case !slices.Contains(disabled, p.info.Manifest.ID):
			h.registerLanguages(p)
		case p.holds == 0:
			h.unregisterLanguages(p)
		}
	}
}

// holdLanguages counts a scope that has the extension enabled, and registers its languages if they are not.
func (h *Host) holdLanguages(extensionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.packs[extensionID]; p != nil && h.languages != nil {
		p.holds++
		h.registerLanguages(p)
	}
}

// releaseLanguages counts a scope that no longer has the extension enabled, and takes its languages out with the last.
func (h *Host) releaseLanguages(extensionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.packs[extensionID]
	if p == nil || h.languages == nil {
		return
	}
	p.holds--
	if p.holds <= 0 {
		p.holds = 0
		h.unregisterLanguages(p)
	}
}

// LanguagePack tells whether the extension adds languages and, if so, whether they are in the registry now.
func (h *Host) LanguagePack(extensionID string) (isPack, registered bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.packs[extensionID]
	if p == nil {
		return false, false
	}
	return true, p.registered
}
