package git

import "strings"

// The ignored files that a new worktree gets a copy of (git.worktreeIncludeFiles, SPEC §10.30 "Worktrees"): which of the files git
// ignores in the main work tree match the patterns of the setting, with a wholly ignored folder collapsed into one entry.
//
// Ported from VS Code: extensions/git/src/worktreeInclude.ts

// SanitizeWorktreeIncludePatterns removes the patterns that contain a line break. Each entry of git.worktreeIncludeFiles must stay one
// .gitignore line: a line break in it would inject more patterns (a "!" negation).
func SanitizeWorktreeIncludePatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if !strings.ContainsAny(p, "\r\n") {
			out = append(out, p)
		}
	}
	return out
}

// ResolveWorktreeIncludePaths selects the ignored paths to copy into a worktree from the NUL separated outputs of "git ls-files", and
// collapses a wholly ignored folder whose every ignored file is included into one recursive entry.
//
// ignoredOutput is "git ls-files --others --ignored --exclude-standard -z" in the repository, includedOutput the same with
// "--exclude-from=<patterns>" in place of the standard sources, directoryOutput the first with "--directory --no-empty-directory",
// worktreeOutput "git ls-files -z" in the new worktree, and excludedFolders the folders (relative to the repository) that are linked
// into the worktree already. The paths are relative to the repository with forward slashes; a folder has no trailing slash.
func ResolveWorktreeIncludePaths(ignoredOutput, includedOutput, directoryOutput, worktreeOutput string, excludedFolders []string) []string {
	ignoredFiles := splitNulString(ignoredOutput)
	if len(ignoredFiles) == 0 {
		return nil
	}
	includedFiles := setOf(splitNulString(includedOutput))
	var wholeList []string
	wholeDirectories := map[string]bool{}
	for _, e := range splitNulString(directoryOutput) {
		if strings.HasSuffix(e, "/") && !wholeDirectories[e] {
			wholeDirectories[e] = true
			wholeList = append(wholeList, e)
		}
	}
	worktreeFiles := setOf(splitNulString(worktreeOutput))
	excludedDirectories := map[string]bool{}
	for _, f := range excludedFolders {
		if !strings.HasSuffix(f, "/") {
			f += "/"
		}
		excludedDirectories[f] = true
	}
	// every ancestor folder of a tracked path, with the trailing slash, to check a source against the shape of its destination
	worktreeDirectories := map[string]bool{}
	for file := range worktreeFiles {
		for i := 0; i < len(file); i++ {
			if file[i] == '/' {
				worktreeDirectories[file[:i+1]] = true
			}
		}
	}
	var matchedFiles []string
	nonCollapsible := map[string]bool{}
	for _, file := range ignoredFiles {
		if includedFiles[file] && containingDirectory(file, excludedDirectories) == "" && !worktreePathCollides(file, worktreeFiles, worktreeDirectories) {
			matchedFiles = append(matchedFiles, file)
		} else if len(wholeDirectories) > 0 {
			if dir := containingDirectory(file, wholeDirectories); dir != "" {
				nonCollapsible[dir] = true
			}
		}
	}
	if len(matchedFiles) == 0 {
		return nil
	}
	collapsed := map[string]bool{}
	var out []string
	for _, dir := range wholeList {
		if !nonCollapsible[dir] {
			collapsed[dir] = true
			out = append(out, strings.TrimSuffix(dir, "/"))
		}
	}
	for _, file := range matchedFiles {
		if len(collapsed) == 0 || containingDirectory(file, collapsed) == "" {
			out = append(out, file)
		}
	}
	return out
}

// splitNulString splits NUL separated output and drops the empty entries.
func splitNulString(s string) []string {
	var out []string
	for _, e := range strings.Split(s, "\x00") {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// setOf makes a set of strings.
func setOf(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, e := range list {
		m[e] = true
	}
	return m
}

// containingDirectory returns the shallowest folder of directories (each with a trailing slash) that contains file, "" when none does.
// It tries each slash of the path, so the cost grows with the depth of the path and not with the number of folders.
func containingDirectory(file string, directories map[string]bool) string {
	for i := 0; i < len(file); i++ {
		if file[i] == '/' {
			if prefix := file[:i+1]; directories[prefix] {
				return prefix
			}
		}
	}
	return ""
}

// worktreePathCollides says whether copying a source path would overwrite a tracked path of the worktree or clash with the shape of its
// destination: the destination is a tracked file, or a tracked folder (a file cannot take its place), or one of its ancestors is a
// tracked file (the folders leading to it cannot be made).
func worktreePathCollides(file string, worktreeFiles, worktreeDirectories map[string]bool) bool {
	if worktreeFiles[file] || worktreeDirectories[file+"/"] {
		return true
	}
	for i := 0; i < len(file); i++ {
		if file[i] == '/' && worktreeFiles[file[:i]] {
			return true
		}
	}
	return false
}
