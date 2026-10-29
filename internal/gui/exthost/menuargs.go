package exthost

// IsExtensionCommand tells whether a command is one that the manifest of an extension of the scope declares (an extension that is
// turned off or failed has none). The window uses it to give the commands of extensions, and only those, the files the context
// menu of the Explorer was run on (extension.ExplorerArgs): the editor's own commands take no such argument.
func (s *Scope) IsExtensionCommand(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range s.slots {
		if !liveSlot(sl) {
			continue
		}
		for _, c := range sl.info.Manifest.Contributes.Commands {
			if c.ID == id {
				return true
			}
		}
	}
	return false
}
