package extensionsui

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// checked is a requirement row of the tests: an extension, a requirement and what its check found.
func checked(ext, name string, optional bool, res extension.RequirementResult) exthost.RequirementRow {
	return exthost.RequirementRow{ExtensionID: "emetos." + ext, ExtensionName: ext, Requirement: extension.Requirement{ID: name, Name: name, Optional: optional, Purpose: "for " + name}, Result: res}
}

var (
	found    = extension.RequirementResult{State: extension.RequirementFound, Path: "/usr/bin/x", Version: "1.0"}
	missing  = extension.RequirementResult{State: extension.RequirementMissing, Detail: "not in PATH"}
	unusable = extension.RequirementResult{State: extension.RequirementUnusable, Path: "/usr/bin/git", Version: "2.25.1", Detail: "git 2.25.1 is older than 2.30"}
	unknown  = extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check took longer than 10s"}
)

func names(rows []exthost.RequirementRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Requirement.ID)
	}
	return out
}

func TestGroupRequirements(t *testing.T) {
	rows := []exthost.RequirementRow{
		checked("Go", "dlv", true, missing),
		checked("Go", "gopls", false, missing),
		checked("Go", "go", false, found),
		checked("Git", "git", false, unusable),
		checked("Camera", "camera", true, unknown),
		{ExtensionID: "emetos.Slow", ExtensionName: "Slow", Requirement: extension.Requirement{ID: "engine", Name: "engine"}, Checking: true},
		checked("Redis", "keyring", true, missing),
		checked("Redis", "cli", true, found),
		checked("Docker", "engine", false, missing),
	}
	g := GroupRequirements(rows, []string{"emetos.Redis", "emetos.Docker", "emetos.Nothing"})
	tests := []struct {
		name      string
		got, want []string
	}{
		{"problems, the required ones first", names(g.Problems), []string{"gopls", "git", "dlv"}},
		{"not checked", names(g.Unknown), []string{"camera", "engine"}},
		{"found", names(g.Found), []string{"go"}},
	}
	for _, tt := range tests {
		if len(tt.got) != len(tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, tt.got, tt.want)
			continue
		}
		for i := range tt.got {
			if tt.got[i] != tt.want[i] {
				t.Errorf("%s: %v, want %v", tt.name, tt.got, tt.want)
				break
			}
		}
	}
	if g.Missing != 2 || g.Unusable != 1 || g.NotChecked != 1 || g.Checking != 1 || g.LeftOut != 2 {
		t.Errorf("counts %+v", g)
	}
}

func TestSummaryAndLeftOutTexts(t *testing.T) {
	tests := []struct {
		name string
		g    SystemCheckGroups
		want string
	}{
		{"nothing", SystemCheckGroups{}, "No enabled extension needs anything from the system."},
		{"all found", GroupRequirements([]exthost.RequirementRow{checked("Go", "go", false, found), checked("Go", "gopls", false, found)}, nil), "2 found"},
		{"every state", GroupRequirements([]exthost.RequirementRow{
			checked("Go", "gopls", false, missing), checked("Go", "dlv", true, missing), checked("Git", "git", false, unusable),
			checked("Camera", "camera", true, unknown), checked("Go", "go", false, found),
		}, nil), "2 missing, 1 cannot be used, 1 not checked, 1 found"},
		{"checking", GroupRequirements([]exthost.RequirementRow{
			checked("Go", "go", false, found), {Requirement: extension.Requirement{ID: "gopls"}, Checking: true}, {Requirement: extension.Requirement{ID: "dlv"}},
		}, nil), "Checking... 1 of 3 done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SummaryText(tt.g); got != tt.want {
				t.Errorf("SummaryText = %q, want %q", got, tt.want)
			}
		})
	}
	for n, want := range map[int]string{
		0: "",
		1: "1 turned-off extension is not checked; see the Extensions page.",
		3: "3 turned-off extensions are not checked; see the Extensions page.",
	} {
		if got := LeftOutText(n); got != want {
			t.Errorf("LeftOutText(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestReportMarkdown(t *testing.T) {
	g := GroupRequirements([]exthost.RequirementRow{
		checked("Go", "dlv", true, missing),
		checked("Go", "gopls", false, missing),
		checked("Go", "go", false, found),
		checked("Git", "git", false, unusable),
		checked("Weird|Name", "odd", false, extension.RequirementResult{State: extension.RequirementFound, Path: "/a|b", Detail: "line one\nline two"}),
		checked("Redis", "keyring", true, missing),
	}, []string{"emetos.Redis"})
	want := "# Emetos System Check\n" +
		"\n" +
		"System: Fedora Linux 45 (Workstation Edition), fedora, linux/amd64\n" +
		"\n" +
		"2 missing, 1 cannot be used, 2 found\n" +
		"\n" +
		"## Missing or cannot be used\n" +
		"\n" +
		"| Requirement | Extension | Result | Path | Version | Detail |\n" +
		"| --- | --- | --- | --- | --- | --- |\n" +
		"| gopls | Go | missing |  |  | not in PATH |\n" +
		"| git | Git | cannot be used | /usr/bin/git | 2.25.1 | git 2.25.1 is older than 2.30 |\n" +
		"| dlv (optional) | Go | missing |  |  | not in PATH |\n" +
		"\n" +
		"## Not checked\n" +
		"\n" +
		"None.\n" +
		"\n" +
		"## Found\n" +
		"\n" +
		"| Requirement | Extension | Result | Path | Version | Detail |\n" +
		"| --- | --- | --- | --- | --- | --- |\n" +
		"| go | Go | found | /usr/bin/x | 1.0 |  |\n" +
		"| odd | Weird\\|Name | found | /a\\|b |  | line one line two |\n" +
		"\n" +
		"1 turned-off extension is not checked.\n"
	if got := ReportMarkdown("Fedora Linux 45 (Workstation Edition), fedora, linux/amd64", g); got != want {
		t.Errorf("ReportMarkdown =\n%s\nwant\n%s", got, want)
	}
}
