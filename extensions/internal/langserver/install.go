package langserver

import "github.com/rm4n0s/emetos/extension"

// Install says how to get a program (SPEC §10.34: Emetos never installs a server, D47; it tells the user how). It is the extension
// contract's, which the requirements of SPEC §10.36 share.
type Install = extension.Install

// InstallCommand is a command that installs a program.
type InstallCommand = extension.InstallCommand

// The families of systems an InstallCommand is for.
const (
	FamilyAny    = extension.FamilyAny
	FamilyFedora = extension.FamilyFedora
	FamilyDebian = extension.FamilyDebian
	FamilyArch   = extension.FamilyArch
	FamilySuse   = extension.FamilySuse
	FamilyMacOS  = extension.FamilyMacOS
)

// Family tells the family of the system from the text of /etc/os-release: its ID and ID_LIKE, "" when it is none of ours.
func Family(osRelease string) string { return extension.OSFamily(osRelease) }

// ReadOSRelease reads the file that names the system, "" when there is none.
func ReadOSRelease() string { return extension.ReadOSRelease() }
