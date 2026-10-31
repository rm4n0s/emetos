package helper

import (
	"net/url"
	"regexp"
	"strings"
)

// PromptKind says what a prompt of git or ssh asks for.
type PromptKind int

// The kinds of prompt.
const (
	// PromptInput is any other question that wants a line of text.
	PromptInput PromptKind = iota
	// PromptUsername is git's Username for 'https://host': .
	PromptUsername
	// PromptPassword is git's Password for 'https://user@host': , or the password of ssh.
	PromptPassword
	// PromptPassphrase is the passphrase of a key (ssh, ssh-add, ssh-keygen when git signs with ssh).
	PromptPassphrase
	// PromptConfirm is a question to answer with yes or no: the host key of ssh.
	PromptConfirm
	// PromptNotice is a message that wants no answer (SSH_ASKPASS_PROMPT=none: "Confirm user presence for key ...").
	PromptNotice
)

// Prompt is a prompt of git or ssh, classified.
type Prompt struct {
	// Kind says what is asked.
	Kind PromptKind
	// Text is the prompt as git or ssh wrote it, without the colon and the space at its end; for a host key question the line that
	// asks.
	Text string
	// Target is where the secret goes: "https://host" for git (the user name, the path and the query are left out), "user@host" for the
	// password of ssh, the host of a host key question; empty when the prompt names none.
	Target string
	// File is the file of the key whose passphrase is asked.
	File string
	// Fingerprint is the fingerprint of the host key in a host key question.
	Fingerprint string
	// Secret says the answer is typed as a password, with dots.
	Secret bool
}

var (
	reGitPrompt   = regexp.MustCompile(`^(Username|Password) for '([^']*)'`)
	rePassphrase  = regexp.MustCompile(`(?i)\bpass ?phrase\b`)
	rePassFile    = regexp.MustCompile(`(?i)\bfor(?: key)?\s+(?:'([^']+)'|"([^"]+)"|(/\S.*?)):?\s*$`)
	rePassword    = regexp.MustCompile(`(?i)(?:^|\s)password\b`)
	reSSHPassword = regexp.MustCompile(`^(\S+)'s password\b`)
	reHostKey     = regexp.MustCompile(`(?i)continue connecting|type 'yes', 'no'`)
	reHostName    = regexp.MustCompile(`(?i)authenticity of host '([^']+)'`)
	reFingerprint = regexp.MustCompile(`(?im)key fingerprint is:?\s+(\S+?)\.?\s*$`)
	reSecretWord  = regexp.MustCompile(`(?i)pass(?:word|phrase)|\bpin\b|secret|token`)
)

// ParsePrompt classifies the prompt of an askpass request from the real texts of git and ssh. kind is KindAskpass or KindSSHAskpass,
// argv the arguments of the program (git and ssh give the whole prompt as one argument; several are joined with spaces) and
// sshPrompt the value of SSH_ASKPASS_PROMPT ("confirm", "none" or empty).
//
// Ported in idea from VS Code: extensions/git/src/askpass.ts (handleAskpass, handleSSHAskpass).
func ParsePrompt(kind Kind, argv []string, sshPrompt string) Prompt {
	text := strings.TrimRight(strings.Join(argv, " "), " \t\r\n")
	p := Prompt{Text: strings.TrimSuffix(strings.TrimSpace(text), ":")}
	if kind == KindAskpass {
		if m := reGitPrompt.FindStringSubmatch(text); m != nil {
			p.Text = strings.TrimSuffix(strings.TrimSpace(text), ":")
			p.Target = remoteOf(m[2])
			if m[1] == "Username" {
				p.Kind = PromptUsername
			} else {
				p.Kind, p.Secret = PromptPassword, true
			}
			return p
		}
	}
	switch {
	case sshPrompt == "none":
		p.Kind = PromptNotice
	case sshPrompt == "confirm" || reHostKey.MatchString(text):
		p.Kind = PromptConfirm
		if m := reHostName.FindStringSubmatch(text); m != nil {
			p.Target = m[1]
		}
		if m := reFingerprint.FindStringSubmatch(text); m != nil {
			p.Fingerprint = m[1]
		}
		// the line that asks is the last one
		if lines := strings.Split(text, "\n"); len(lines) > 1 {
			p.Text = strings.TrimSuffix(strings.TrimSpace(lines[len(lines)-1]), ":")
		}
	case rePassphrase.MatchString(text):
		p.Kind, p.Secret = PromptPassphrase, true
		if m := rePassFile.FindStringSubmatch(text); m != nil {
			p.File = firstNonEmpty(m[1], m[2], m[3])
		}
	case reSSHPassword.MatchString(text):
		p.Kind, p.Secret = PromptPassword, true
		p.Target = reSSHPassword.FindStringSubmatch(text)[1]
	case rePassword.MatchString(text):
		p.Kind, p.Secret = PromptPassword, true
	default:
		p.Kind = PromptInput
		p.Secret = reSecretWord.MatchString(text)
	}
	return p
}

// remoteOf is the "scheme://host" of the URL in a prompt of git, without the user name, the path and the query; the text itself when it
// is not a URL.
func remoteOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	return u.Scheme + "://" + u.Host
}

func firstNonEmpty(list ...string) string {
	for _, s := range list {
		if s != "" {
			return s
		}
	}
	return ""
}
