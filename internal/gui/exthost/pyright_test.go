//go:build emetos_pyright

package exthost

import "testing"

// The tests against the real basedpyright and Pyright (SPEC §10.34 "Tests", step 24b.4); python_servers_test.go has the rig.

func TestBasedpyrightAnswersDiagnosticsHoverCompletionDefinitionAndRename(t *testing.T) {
	checkTheFeatures(t, basedpyrightServer)
}

func TestPyrightAnswersDiagnosticsHoverCompletionDefinitionAndRename(t *testing.T) {
	checkTheFeatures(t, pyrightServer)
}

func TestBasedpyrightOrganizesTheImports(t *testing.T) { checkOrganizeImports(t, basedpyrightServer) }

func TestPyrightOrganizesTheImports(t *testing.T) { checkOrganizeImports(t, pyrightServer) }

func TestChoosingTheInterpreterRestartsBasedpyrightWhichThenFindsThePackage(t *testing.T) {
	checkTheInterpreterDecidesTheImports(t, basedpyrightServer)
}

func TestChoosingTheInterpreterRestartsPyrightWhichThenFindsThePackage(t *testing.T) {
	checkTheInterpreterDecidesTheImports(t, pyrightServer)
}
