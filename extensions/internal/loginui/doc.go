// Package loginui is the questions the login of a client asks its user (SPEC §10.18 "As built (step 15.2)"): the passphrase of a
// key file, the fingerprint of a host that is met for the first time, and the warning that the key of a host changed. They are
// widgets that the tab of step 15.4 puts over its content, and a Prompter that gives extensions/internal/sshlogin the functions
// it calls (a PassphraseFunc and a ConfirmHostFunc): the login runs on a goroutine, the Prompter shows the question on the UI
// thread and the goroutine waits for the answer.
package loginui
