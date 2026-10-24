package workspace

// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (FileSorter)

// comparer is the order of the children of a folder for the configuration.
func (c Config) comparer() func(a, b *Item) int {
	names, exts := CompareFileNamesDefault, CompareFileExtensionsDefault
	switch c.Lexicographic {
	case LexUpper:
		names, exts = CompareFileNamesUpper, CompareFileExtensionsUpper
	case LexLower:
		names, exts = CompareFileNamesLower, CompareFileExtensionsLower
	case LexUnicode:
		names, exts = CompareFileNamesUnicode, CompareFileExtensionsUnicode
	}
	order, reverse := c.SortOrder, c.Reverse
	return func(a, b *Item) int {
		if reverse {
			a, b = b, a
		}
		// folders
		switch order {
		case SortType:
			switch {
			case a.isDir && !b.isDir:
				return -1
			case b.isDir && !a.isDir:
				return 1
			case a.isDir && b.isDir:
				return names(a.name, b.name)
			}
		case SortFilesFirst:
			switch {
			case a.isDir && !b.isDir:
				return 1
			case b.isDir && !a.isDir:
				return -1
			}
		case SortMixed:
			// files and folders are not told apart
		default: // default, modified, foldersNestsFiles (nesting is not built)
			switch {
			case a.isDir && !b.isDir:
				return -1
			case b.isDir && !a.isDir:
				return 1
			}
		}
		// files
		switch order {
		case SortType:
			return exts(a.name, b.name)
		case SortModified:
			if !a.mtime.Equal(b.mtime) {
				if a.mtime.Before(b.mtime) {
					return 1
				}
				return -1
			}
			return names(a.name, b.name)
		default:
			return names(a.name, b.name)
		}
	}
}
