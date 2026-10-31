package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

// The three flags of the programs git runs are answered before the command line of the editor is read (step 24.9).
func TestTheFlagsOfGitsProgramsAreServedBeforeAnythingElse(t *testing.T) {
	dir, err := os.MkdirTemp("", "hm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	token := strings.Repeat("ab", 32)
	socket := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				if !strings.Contains(string(buf[:n]), token) {
					return
				}
				_, _ = c.Write([]byte(`{"ok":true,"value":"ada"}` + "\n"))
			}()
		}
	}()
	env := []string{helper.EnvSocket + "=" + socket, helper.EnvToken + "=" + token}

	tests := []struct {
		name       string
		args       []string
		wantHandle bool
		wantCode   int
		wantOut    string
	}{
		{"askpass", []string{"--git-askpass", "Username for 'https://h': "}, true, 0, "ada\n"},
		{"ssh askpass", []string{"--git-ssh-askpass", "Enter passphrase for key '/k': "}, true, 0, "ada\n"},
		{"editor", []string{"--git-editor", ".git/COMMIT_EDITMSG"}, true, 0, ""},
		{"a normal command line", []string{"--version"}, false, 0, ""},
		{"a file", []string{"main.go"}, false, 0, ""},
		{"nothing", nil, false, 0, ""},
		{"the flag is not the first argument", []string{"main.go", "--git-askpass"}, false, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code, handled := helperMain(tc.args, env, strings.NewReader(""), &out, &errOut)
			if handled != tc.wantHandle || code != tc.wantCode || out.String() != tc.wantOut {
				t.Errorf("handled %v, exit %d, stdout %q, stderr %q; want %v, %d, %q", handled, code, out.String(), errOut.String(), tc.wantHandle, tc.wantCode, tc.wantOut)
			}
		})
	}

	t.Run("a server that is gone fails the program", func(t *testing.T) {
		var out, errOut bytes.Buffer
		code, handled := helperMain([]string{"--git-askpass", "Password for 'https://h': "}, []string{helper.EnvSocket + "=" + filepath.Join(dir, "gone.sock")},
			strings.NewReader(""), &out, &errOut)
		if !handled || code != 1 || out.Len() != 0 || errOut.Len() == 0 {
			t.Errorf("handled %v, exit %d, stdout %q, stderr %q", handled, code, out.String(), errOut.String())
		}
	})
}
