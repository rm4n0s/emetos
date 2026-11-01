package langserver

import "testing"

func TestFamily(t *testing.T) {
	tests := []struct{ name, text, want string }{
		{"Fedora", "NAME=\"Fedora Linux\"\nID=fedora\nVERSION_ID=44\n", FamilyFedora},
		{"Rocky says rhel", "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", FamilyFedora},
		{"Ubuntu", "ID=ubuntu\nID_LIKE=debian\n", FamilyDebian},
		{"Debian", "ID=debian\n", FamilyDebian},
		{"Mint by its parents", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", FamilyDebian},
		{"Arch", "ID=arch\n", FamilyArch},
		{"Manjaro", "ID=manjaro\nID_LIKE=arch\n", FamilyArch},
		{"openSUSE", "ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n", FamilySuse},
		{"unknown", "ID=nixos\n", ""},
		{"empty", "", ""},
		{"a line with no equals sign", "ID\nfoo=bar\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Family(tt.text); got != tt.want {
				t.Errorf("Family = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandFor(t *testing.T) {
	cargo := InstallCommand{Family: FamilyAny, Command: "rustup component add rust-analyzer"}
	dnf := InstallCommand{Family: FamilyFedora, Command: "sudo dnf install clang-tools-extra"}
	apt := InstallCommand{Family: FamilyDebian, Command: "sudo apt install clangd"}
	tests := []struct {
		name     string
		commands []InstallCommand
		family   string
		want     InstallCommand
		ok       bool
	}{
		{"the family wins", []InstallCommand{cargo, dnf, apt}, FamilyDebian, apt, true},
		{"any system when the family has none", []InstallCommand{dnf, cargo}, FamilyArch, cargo, true},
		{"any system for an unknown family", []InstallCommand{dnf, cargo}, "", cargo, true},
		{"the first when nothing fits", []InstallCommand{dnf, apt}, FamilyArch, dnf, true},
		{"none", nil, FamilyFedora, InstallCommand{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Install{Commands: tt.commands}.CommandFor(tt.family)
			if got != tt.want || ok != tt.ok {
				t.Errorf("CommandFor = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}
