//go:build emetos_pylsp

package exthost

import "testing"

// The tests against the real python-lsp-server (SPEC §10.34 "Tests", step 24b.4); python_servers_test.go has the rig.

func TestPylspAnswersDiagnosticsHoverCompletionDefinitionAndRename(t *testing.T) {
	checkTheFeatures(t, pylspServer)
}
