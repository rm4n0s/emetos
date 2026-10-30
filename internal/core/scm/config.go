package scm

// ConfigKeys are the settings that make up a Config; the host calls Model.SetConfig again when one of them changes.
var ConfigKeys = [...]string{
	"scm.countBadge", "scm.providerCountBadge", "scm.defaultViewMode", "scm.defaultViewSortKey", "scm.compactFolders",
	"scm.repositories.selectionMode", "scm.repositories.sortOrder",
}

// ConfigFromSettings reads a Config from a function that gives the value of a setting (a string or a bool, as the settings
// store has them); a value of another type or an unknown one is the default.
func ConfigFromSettings(get func(key string) any) Config {
	str := func(key string) string { s, _ := get(key).(string); return s }
	c := DefaultConfig()
	c.CountBadge = CountBadge(str("scm.countBadge"))
	c.ProviderCountBadge = ProviderCountBadge(str("scm.providerCountBadge"))
	c.DefaultViewMode = ViewMode(str("scm.defaultViewMode"))
	c.DefaultViewSortKey = ParseSortKey(str("scm.defaultViewSortKey"))
	if v, ok := get("scm.compactFolders").(bool); ok {
		c.CompactFolders = v
	}
	c.SelectionMode = SelectionMode(str("scm.repositories.selectionMode"))
	if s := str("scm.repositories.sortOrder"); s != "" {
		c.RepositoriesSortOrder = s
	}
	return normalizeConfig(c)
}
