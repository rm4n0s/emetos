package containerui

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Ported from vscode-containers: extensions/vscode-containers/package.json (containers.commands.build, run, runInteractive, attach,
// composeUp, composeDown, composeUpSubset, composeDownSubset and composeLogs: the default templates) and
// src/commands/selectCommandTemplate.ts. The templates that the user could change there are not a setting here (SPEC §10.20
// "Not built").

// buildArgv is the command line of Build Image: `docker build --pull --rm -f <file> -t <tag> <context>`.
func buildArgv(cli, dockerfile, tag, context string) []string {
	return []string{cli, "build", "--pull", "--rm", "-f", dockerfile, "-t", tag, context}
}

// pushArgv is the command line of Push.
func pushArgv(cli, ref string) []string { return []string{cli, "push", ref} }

// runInteractiveArgv is the command line of Run Interactive: `docker run --rm -it -p 80:80/tcp <image>`, one -p for each port the
// image exposes ("80/tcp").
func runInteractiveArgv(cli string, exposed []string, image string) []string {
	argv := []string{cli, "run", "--rm", "-it"}
	for _, p := range exposed {
		port, proto, _ := strings.Cut(p, "/")
		if port == "" {
			continue
		}
		spec := port + ":" + port
		if proto != "" {
			spec += "/" + proto
		}
		argv = append(argv, "-p", spec)
	}
	return append(argv, image)
}

// attachShellArgv is the command line of Open Shell: `docker exec -it <id> bash`.
func attachShellArgv(cli, id, shell string) []string { return []string{cli, "exec", "-it", id, shell} }

// composeTarget is what a Compose command works on.
type composeTarget struct {
	// files are the -f files, project the -p name, envFile the --env-file, profiles the --profile names.
	files    []string
	project  string
	envFile  string
	profiles []string
}

// composeArgv writes a Compose command line: the program (base: ["docker", "compose"] or ["docker-compose"]), the profiles, the files,
// the project, the environment file, then the verb with its arguments.
func composeArgv(base []string, t composeTarget, verb ...string) []string {
	argv := slices.Clone(base)
	for _, p := range t.profiles {
		argv = append(argv, "--profile", p)
	}
	for _, f := range t.files {
		argv = append(argv, "-f", f)
	}
	if t.project != "" {
		argv = append(argv, "-p", t.project)
	}
	if t.envFile != "" {
		argv = append(argv, "--env-file", t.envFile)
	}
	return append(argv, verb...)
}

// upVerb is `up [-d] [--build] [services...]`.
func upVerb(detached, build bool, services []string) []string {
	v := []string{"up"}
	if detached {
		v = append(v, "-d")
	}
	if build {
		v = append(v, "--build")
	}
	return append(v, services...)
}

// downVerb is `down [services...]`.
func downVerb(services []string) []string { return append([]string{"down"}, services...) }

// logsVerb is `logs --tail 1000 -f`.
func logsVerb() []string { return []string{"logs", "--tail", "1000", "-f"} }

// configVerb is `config --services` or `config --profiles`: the names, one on a line.
func configVerb(kind string) []string { return []string{"config", "--" + kind} }

// imageNamePart makes a valid image name of a hint: lowercase letters and digits only (Container Tools' getValidImageName).
func imageNamePart(hint string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(hint) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "image"
	}
	return b.String()
}

// defaultTag is the tag that Build Image proposes: the name of the folder of the Dockerfile (that of the workspace for one at its
// root), with latest.
func defaultTag(folder, dockerfile string) string {
	dir := filepath.Dir(dockerfile)
	if dir == folder || dir == "." || dir == "" {
		dir = folder
	}
	return imageNamePart(filepath.Base(dir)) + ":latest"
}

// imageRefPattern is what is accepted as the name of an image to build or push: the letters, digits and the separators of a
// reference (registry:port/name:tag@digest); the engine checks the rest.
var imageRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]*$`)

// checkImageRef tells why a text is not an image reference, "" when it may be one.
func checkImageRef(ref string) string {
	switch {
	case strings.TrimSpace(ref) == "":
		return "Enter a name, such as myapp:latest."
	case !imageRefPattern.MatchString(ref):
		return "A name has letters, digits and . _ - / : @ only, and starts with a letter or a digit."
	}
	return ""
}

// isDockerfile tells whether a file name is one of a Dockerfile (the language "dockerfile" of the bundled pack, SPEC §8.1).
func isDockerfile(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	for _, base := range []string{"dockerfile", "containerfile"} {
		if n == base || strings.HasPrefix(n, base+".") || strings.HasSuffix(n, "."+base) {
			return true
		}
	}
	return false
}

// isComposeFile tells whether a file name is one of a Compose file (the language "dockercompose" of the bundled pack).
func isComposeFile(name string) bool {
	n := strings.ToLower(filepath.Base(name))
	if !strings.HasSuffix(n, ".yml") && !strings.HasSuffix(n, ".yaml") {
		return false
	}
	for _, pat := range []string{"compose.yml", "compose.yaml", "compose.*.yml", "compose.*.yaml", "*docker*compose*.yml", "*docker*compose*.yaml"} {
		if ok, _ := filepath.Match(pat, n); ok {
			return true
		}
	}
	return false
}

// relOrAbs writes a path relative to a folder when it is inside it ("." for the folder itself), else absolute.
func relOrAbs(folder, p string) string {
	if folder == "" {
		return p
	}
	rel, err := filepath.Rel(folder, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return rel
}
