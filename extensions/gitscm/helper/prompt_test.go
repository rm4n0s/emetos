package helper

import "testing"

// The prompts are the real texts of git (credential prompts) and of OpenSSH (ssh, ssh-add, ssh-keygen -Y sign), copied from the
// programs' sources and their output; no sshd is started.
func TestParsePrompt(t *testing.T) {
	hostKey := "The authenticity of host 'github.com (140.82.121.4)' can't be established.\n" +
		"ED25519 key fingerprint is SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU.\n" +
		"This key is not known by any other names.\n" +
		"Are you sure you want to continue connecting (yes/no/[fingerprint])? "
	oldHostKey := "The authenticity of host 'example.org (192.0.2.7)' can't be established.\n" +
		"RSA key fingerprint is MD5:aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99.\n" +
		"Are you sure you want to continue connecting (yes/no)? "
	tests := []struct {
		name   string
		kind   Kind
		argv   []string
		prompt string
		want   Prompt
	}{
		{name: "git user name", kind: KindAskpass, argv: []string{"Username for 'https://github.com': "},
			want: Prompt{Kind: PromptUsername, Text: "Username for 'https://github.com'", Target: "https://github.com"}},
		{name: "git password with the user", kind: KindAskpass, argv: []string{"Password for 'https://ada@github.com': "},
			want: Prompt{Kind: PromptPassword, Text: "Password for 'https://ada@github.com'", Target: "https://github.com", Secret: true}},
		{name: "git password, port, loopback", kind: KindAskpass, argv: []string{"Password for 'http://ada@127.0.0.1:41233': "},
			want: Prompt{Kind: PromptPassword, Text: "Password for 'http://ada@127.0.0.1:41233'", Target: "http://127.0.0.1:41233", Secret: true}},
		{name: "git user name with a path (credential.useHttpPath)", kind: KindAskpass, argv: []string{"Username for 'https://example.com/team/repo.git': "},
			want: Prompt{Kind: PromptUsername, Text: "Username for 'https://example.com/team/repo.git'", Target: "https://example.com"}},
		{name: "git password with a secret in the URL is never shown as the target", kind: KindAskpass, argv: []string{"Password for 'https://ada:s3cret@example.com': "},
			want: Prompt{Kind: PromptPassword, Text: "Password for 'https://ada:s3cret@example.com'", Target: "https://example.com", Secret: true}},
		{name: "git prompt split in words", kind: KindAskpass, argv: []string{"Password", "for", "'https://ada@github.com':"},
			want: Prompt{Kind: PromptPassword, Text: "Password for 'https://ada@github.com'", Target: "https://github.com", Secret: true}},
		{name: "ssh passphrase for a key", kind: KindSSHAskpass, argv: []string{"Enter passphrase for key '/home/u/.ssh/id_ed25519': "},
			want: Prompt{Kind: PromptPassphrase, Text: "Enter passphrase for key '/home/u/.ssh/id_ed25519'", File: "/home/u/.ssh/id_ed25519", Secret: true}},
		{name: "ssh passphrase with a space in the path", kind: KindSSHAskpass, argv: []string{"Enter passphrase for key '/home/a b/.ssh/id rsa': "},
			want: Prompt{Kind: PromptPassphrase, Text: "Enter passphrase for key '/home/a b/.ssh/id rsa'", File: "/home/a b/.ssh/id rsa", Secret: true}},
		{name: "ssh-add passphrase", kind: KindSSHAskpass, argv: []string{"Enter passphrase for /home/u/.ssh/id_rsa: "},
			want: Prompt{Kind: PromptPassphrase, Text: "Enter passphrase for /home/u/.ssh/id_rsa", File: "/home/u/.ssh/id_rsa", Secret: true}},
		{name: "ssh-keygen signing passphrase (double quotes)", kind: KindSSHAskpass, argv: []string{`Enter passphrase for "/home/u/.ssh/id_ed25519": `},
			want: Prompt{Kind: PromptPassphrase, Text: `Enter passphrase for "/home/u/.ssh/id_ed25519"`, File: "/home/u/.ssh/id_ed25519", Secret: true}},
		{name: "commit signing without a file", kind: KindSSHAskpass, argv: []string{"Enter passphrase: "},
			want: Prompt{Kind: PromptPassphrase, Text: "Enter passphrase", Secret: true}},
		{name: "bad passphrase, again", kind: KindSSHAskpass, argv: []string{"Bad passphrase, try again for /home/u/.ssh/id_rsa: "},
			want: Prompt{Kind: PromptPassphrase, Text: "Bad passphrase, try again for /home/u/.ssh/id_rsa", File: "/home/u/.ssh/id_rsa", Secret: true}},
		{name: "ssh password", kind: KindSSHAskpass, argv: []string{"ada@example.org's password: "},
			want: Prompt{Kind: PromptPassword, Text: "ada@example.org's password", Target: "ada@example.org", Secret: true}},
		{name: "ssh PIN of a security key", kind: KindSSHAskpass, argv: []string{"Enter PIN for ECDSA-SK key /home/u/.ssh/id_ecdsa_sk: "},
			want: Prompt{Kind: PromptInput, Text: "Enter PIN for ECDSA-SK key /home/u/.ssh/id_ecdsa_sk", Secret: true}},
		{name: "ssh host key", kind: KindSSHAskpass, argv: []string{hostKey}, prompt: "confirm",
			want: Prompt{Kind: PromptConfirm, Text: "Are you sure you want to continue connecting (yes/no/[fingerprint])?", Target: "github.com (140.82.121.4)",
				Fingerprint: "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"}},
		{name: "ssh host key of an old ssh, no SSH_ASKPASS_PROMPT", kind: KindSSHAskpass, argv: []string{oldHostKey},
			want: Prompt{Kind: PromptConfirm, Text: "Are you sure you want to continue connecting (yes/no)?", Target: "example.org (192.0.2.7)",
				Fingerprint: "MD5:aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99"}},
		{name: "ssh asks again", kind: KindSSHAskpass, argv: []string{"Please type 'yes', 'no' or the fingerprint: "}, prompt: "confirm",
			want: Prompt{Kind: PromptConfirm, Text: "Please type 'yes', 'no' or the fingerprint"}},
		{name: "ssh message without an answer", kind: KindSSHAskpass, argv: []string{"Confirm user presence for key ECDSA-SK SHA256:abc"}, prompt: "none",
			want: Prompt{Kind: PromptNotice, Text: "Confirm user presence for key ECDSA-SK SHA256:abc"}},
		{name: "a client certificate asked by curl through git", kind: KindAskpass, argv: []string{"Enter PEM pass phrase:"},
			want: Prompt{Kind: PromptPassphrase, Text: "Enter PEM pass phrase", Secret: true}},
		{name: "unknown prompt", kind: KindAskpass, argv: []string{"Verification code: "},
			want: Prompt{Kind: PromptInput, Text: "Verification code"}},
		{name: "no prompt", kind: KindAskpass, argv: nil, want: Prompt{Kind: PromptInput}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParsePrompt(tc.kind, tc.argv, tc.prompt)
			if got != tc.want {
				t.Errorf("ParsePrompt(%q) =\n %+v\nwant\n %+v", tc.argv, got, tc.want)
			}
		})
	}
}
