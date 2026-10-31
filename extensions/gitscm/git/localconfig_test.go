package git

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

func TestParseConfigFile(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []ConfigEntry
	}{
		{"sections, keys without case, comments", "# a comment\n[Core]\n\tBare = false ; trailing\n\tFileMode = true\n; another\n[user]\nname = Ada Example\n",
			[]ConfigEntry{{"core.bare", "false", true}, {"core.filemode", "true", true}, {"user.name", "Ada Example", true}}},
		{"a subsection keeps its case", "[remote \"Origin\"]\n\turl = https://example.com/r.git\n[branch.Main]\nremote = Origin\n",
			[]ConfigEntry{{"remote.Origin.url", "https://example.com/r.git", true}, {"branch.Main.remote", "Origin", true}}},
		{"a key with no value", "[core]\n\tfsmonitor\n", []ConfigEntry{{"core.fsmonitor", "", false}}},
		{"quotes and escapes", "[alias]\n\tst = \"!sh -c \\\"git status ; echo done\\\"\"\n\tt = \"tab\\there\"\n\tspaces = \"  keep  \"  \n",
			[]ConfigEntry{{"alias.st", `!sh -c "git status ; echo done"`, true}, {"alias.t", "tab\there", true}, {"alias.spaces", "  keep  ", true}}},
		{"an unquoted comment ends the value", "[a]\nb = value # not this\nc = x;y\n", []ConfigEntry{{"a.b", "value", true}, {"a.c", "x", true}}},
		{"a line continuation", "[a]\nlong = one \\\ntwo\nnext = 1\n", []ConfigEntry{{"a.long", "one two", true}, {"a.next", "1", true}}},
		{"two backslashes are not a continuation", "[a]\np = c:\\\\\nq = 2\n", []ConfigEntry{{"a.p", `c:\`, true}, {"a.q", "2", true}}},
		{"a section and a key on one line", "[core] bare = true\n", []ConfigEntry{{"core.bare", "true", true}}},
		{"includes are listed, not followed", "[include]\n\tpath = /etc/evil\n[includeIf \"gitdir:/x/\"]\n\tpath = other\n",
			[]ConfigEntry{{"include.path", "/etc/evil", true}, {"includeif.gitdir:/x/.path", "other", true}}},
		{"a key before any section is dropped", "orphan = 1\n[a]\nb = 2\n", []ConfigEntry{{"a.b", "2", true}}},
		{"a broken header is skipped with its keys", "[a\nb = 1\n[c]\nd = 2\n", []ConfigEntry{{"c.d", "2", true}}},
		{"windows line ends", "[a]\r\nb = 1\r\n", []ConfigEntry{{"a.b", "1", true}}},
		{"empty", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseConfigFile(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestPrograms(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"nothing runs", "[core]\n\tbare = false\n\tfsmonitor = true\n[user]\n\tname = Ada\n[alias]\n\tst = status -s\n[credential]\n\thelper = store\n", nil},
		{"fsmonitor that is a boolean", "[core]\nfsmonitor = false\n[core]\nfsmonitor = yes\nfsmonitor\n", nil},
		{"fsmonitor that is a program", "[core]\n\tfsmonitor = .git/hooks/fsmonitor\n", []string{"core.fsmonitor"}},
		{"the core keys, in any case", "[Core]\n\tsshCommand = ssh -i key\n\tHOOKSPATH = .githooks\n\tpager = less\n\teditor = vim\n",
			[]string{"core.sshCommand", "core.hooksPath", "core.pager", "core.editor"}},
		{"credential helpers", "[credential]\n\thelper = !pass-helper\n\thelper = cache\n[credential \"https://example.com\"]\n\thelper = /opt/bin/helper\n\thelper = osxkeychain\n",
			[]string{"credential.helper", "credential.https://example.com.helper"}},
		{"diff and filter", "[diff]\n\texternal = mydiff\n[diff \"word\"]\n\ttextconv = antiword\n\tcommand = tool\n\tbinary = true\n[filter \"lfs\"]\n\tclean = git-lfs clean\n\tsmudge = git-lfs smudge\n\trequired = true\n",
			[]string{"diff.external", "diff.word.textconv", "diff.word.command", "filter.lfs.clean", "filter.lfs.smudge"}},
		{"aliases with a bang", "[alias]\n\tsh = !sh -c 'echo hi'\n\tst = status\n\tbang = \"  !echo\"\n", []string{"alias.sh", "alias.bang"}},
		{"empty values name nothing", "[core]\n\tpager =\n\teditor\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, p := range Programs(ParseConfigFile(tc.text)) {
				got = append(got, p.Key)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReadLocalConfigReadsOnlyTheRepositoryFiles(t *testing.T) {
	e := gittest.NewEnv(t)
	main := e.Init("main")
	main.Write("a.txt", "a\n")
	main.Commit("Add a")
	main.Git("config", "alias.sh", "!echo hello")
	main.Git("remote", "add", "origin", "https://user:secret@example.com/r.git")
	wt := filepath.Join(e.Work, "feature")
	main.Git("worktree", "add", "-q", wt, "-b", "feature")
	main.Git("config", "extensions.worktreeConfig", "true")
	e.Git(wt, "config", "--worktree", "core.editor", "vim")
	// the global file names a program: it must not be read
	if err := os.WriteFile(e.GlobalConfig, []byte("[core]\n\tpager = cat-global\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc, ok := FindRepository(main.Dir)
	if !ok {
		t.Fatal("no repository")
	}
	entries, err := ReadLocalConfig(loc)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range Programs(entries) {
		keys = append(keys, p.Key)
	}
	if !reflect.DeepEqual(keys, []string{"core.hooksPath", "alias.sh"}) { // gittest sets core.hooksPath in every repository
		t.Errorf("the programs of the main repository: %v", keys)
	}
	remotes := RemotesFromConfig(entries)
	if len(remotes) != 1 || remotes[0].Name != "origin" || remotes[0].FetchURL != "https://user:secret@example.com/r.git" {
		t.Errorf("remotes = %+v", remotes)
	}
	wtLoc, ok := FindRepository(wt)
	if !ok {
		t.Fatal("no worktree")
	}
	wtEntries, err := ReadLocalConfig(wtLoc)
	if err != nil {
		t.Fatal(err)
	}
	keys = nil
	for _, p := range Programs(wtEntries) {
		keys = append(keys, p.Key)
	}
	// the worktree shares the config of the main repository and adds its own config.worktree
	if !reflect.DeepEqual(keys, []string{"core.hooksPath", "alias.sh", "core.editor"}) {
		t.Errorf("the programs of the worktree: %v", keys)
	}
}

func TestReadLocalConfigErrors(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	loc := Location{Root: dir, GitDir: gitDir}
	if entries, err := ReadLocalConfig(loc); err != nil || len(entries) != 0 {
		t.Errorf("no config file: %v, %v", entries, err)
	}
	big := "[a]\nk = " + strings.Repeat("x", MaxConfigSize) + "\n"
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadLocalConfig(loc)
	wantRoute(t, err, "ReadLocalConfig->readConfigFile.GitConfigTooLarge")
	// exactly the limit is read
	ok := "[a]\nk = " + strings.Repeat("x", MaxConfigSize-len("[a]\nk = \n")) + "\n"
	if len(ok) != MaxConfigSize {
		t.Fatalf("len = %d", len(ok))
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(ok), 0o644); err != nil {
		t.Fatal(err)
	}
	if entries, err := ReadLocalConfig(loc); err != nil || len(entries) != 1 {
		t.Errorf("a file of the limit's size: %d entries, %v", len(entries), err)
	}
	// a config that is a folder cannot be read
	if err := os.Remove(filepath.Join(gitDir, "config")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(gitDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = ReadLocalConfig(loc)
	wantRoute(t, err, "ReadLocalConfig->readConfigFile.GitConfigReadFailed")
}
