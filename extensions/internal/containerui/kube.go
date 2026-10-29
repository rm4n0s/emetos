package containerui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// Kube (SPEC §10.20 "Podman only"): Generate Kube YAML (`podman kube generate`, opened in an editor), Play Kube (`podman kube play` on a
// YAML file) and Kube Down (`podman kube down`, which asks first). The program is the CLI, so that the user's registries, secrets and
// containers.conf are the ones used.

// kubeSubject is what Generate Kube YAML describes: the containers or pods with these names (all of one kind: `podman kube generate`
// takes both, and tells which by name).
type kubeSubject struct {
	kind  string // "container" or "pod", for the messages
	name  string
	names []string
}

// list returns the names to generate for.
func (k kubeSubject) list() []string {
	if len(k.names) > 0 {
		return k.names
	}
	return []string{k.name}
}

// title names the subject for the title of the editor.
func (k kubeSubject) title() string {
	l := k.list()
	if len(l) == 1 {
		return l[0]
	}
	return plural(len(l), k.kind)
}

// kubeGenerateTimeout bounds `podman kube generate`.
const kubeGenerateTimeout = 60 * time.Second

// maxKubeOutput is the most bytes of generated YAML that are kept.
const maxKubeOutput = 4 << 20

// captured is the output of a program that ran to its end.
type captured struct {
	stdout, stderr string
	code           int
}

// capture runs a program and keeps what it wrote, up to max bytes of each stream (the rest is dropped). It fails as runProcess does.
func capture(ctx context.Context, job cliJob, max int) (captured, error) {
	var out, errOut strings.Builder
	code, err := runProcess(ctx, job, func(stderr bool, line string) {
		b := &out
		if stderr {
			b = &errOut
		}
		if b.Len() < max {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	})
	return captured{stdout: out.String(), stderr: errOut.String(), code: code}, err
}

// generateKube runs `podman kube generate` for containers or pods and opens the YAML in an untitled editor.
func (e *ext) generateKube(subject kubeSubject) {
	if _, ok := e.ready(); !ok {
		return
	}
	cli, err := e.findCLI()
	if err != nil {
		e.api.Notifications().Error("Cannot generate Kube YAML: " + e.cliMissingText())
		return
	}
	argv := append([]string{cli, "kube", "generate"}, subject.list()...)
	go func() {
		ctx, cancel := context.WithTimeout(e.ctx, kubeGenerateTimeout)
		defer cancel()
		res, err := capture(ctx, cliJob{argv: argv, dir: e.folder(), env: e.cliEnviron(), name: e.cfg.cliName()}, maxKubeOutput)
		e.api.Post(func() {
			switch {
			case err != nil:
				e.api.Notifications().Error("Cannot generate Kube YAML for " + subject.title() + ": " + reasonOf(err))
			case res.code != 0:
				e.api.Notifications().Error("Cannot generate Kube YAML for " + subject.title() + ": " + firstLine(res.stderr, "the command ended with status "+strconv.Itoa(res.code)))
			case strings.TrimSpace(res.stdout) == "":
				e.api.Notifications().Warn("Podman generated nothing for " + subject.title() + ".")
			default:
				e.openGenerated("yaml", subject.title()+" (kube)", res.stdout)
			}
		})
	}()
}

// firstLine returns the first line of a text that is not empty, else the fallback.
func firstLine(text, fallback string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return fallback
}

// ---- Play and Down ----

// isKubeFile tells whether a path may be a Kubernetes YAML file: a YAML file that is not a Compose file.
func isKubeFile(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return (ext == ".yaml" || ext == ".yml") && !isComposeFile(p)
}

// kubeOnFiles is Play Kube or Kube Down run on the files the arguments, the document in front or a pick name.
func (e *ext) kubeOnFiles(args any, down bool) {
	e.pickFiles(args, "Kubernetes YAML file", isKubeFile, "yaml", func(paths []string) {
		if down {
			e.kubeDown(paths)
		} else {
			e.kubePlay(paths)
		}
	})
}

// kubePlay runs `podman kube play` on each file, one after the other, in a tab of the panel.
func (e *ext) kubePlay(paths []string) {
	folder, ok := e.needFolder("play Kube YAML")
	if !ok {
		return
	}
	cli, err := e.findCLI()
	if err != nil {
		e.api.Notifications().Error("Cannot play Kube YAML: " + e.cliMissingText())
		return
	}
	var jobs []cliJob
	for _, p := range paths {
		jobs = append(jobs, cliJob{argv: []string{cli, "kube", "play", relOrAbs(folder, p)}, dir: folder, env: e.cliEnviron(), name: e.cfg.cliName()})
	}
	title := composeTitle(folder, paths)
	e.logs.runCLI("Kube Play: "+title, jobs, func(res cliResult) {
		switch {
		case res.canceled:
		case res.err != nil:
			e.api.Notifications().Error("Cannot play " + title + ": " + reasonOf(res.err))
		case !res.ok:
			e.api.Notifications().Error("Kube Play of " + title + " failed. The output is in the " + e.cfg.Name + " Logs tab.")
		default:
			e.api.Notifications().Info("Played " + title + ".")
		}
	})
}

// kubeResource is a thing a Kube YAML file describes that Podman makes pods of.
type kubeResource struct{ kind, name string }

// parseKubeNames reads the YAML text of a Kubernetes file for its pods: the kind and metadata.name of each document. It reads only
// the lines it needs (a top-level kind, then metadata and its name), not YAML in general, and ignores everything else.
func parseKubeNames(text string) []kubeResource {
	var out []kubeResource
	for _, doc := range splitYAMLDocs(text) {
		var kind, name string
		inMeta := false
		for _, line := range strings.Split(doc, "\n") {
			line = strings.TrimRight(line, " \t\r")
			if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				inMeta = false
				if v, ok := strings.CutPrefix(line, "kind:"); ok {
					kind = yamlScalar(v)
				}
				if strings.HasPrefix(line, "metadata:") {
					inMeta = true
				}
				continue
			}
			if inMeta && name == "" {
				if v, ok := strings.CutPrefix(strings.TrimLeft(line, " \t"), "name:"); ok && indentOf(line) <= 4 {
					name = yamlScalar(v)
				}
			}
		}
		if kind != "" && name != "" {
			out = append(out, kubeResource{kind: kind, name: name})
		}
	}
	return out
}

// indentOf is the number of leading spaces of a line.
func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

// splitYAMLDocs cuts a YAML stream at the lines that are only `---`.
func splitYAMLDocs(text string) []string {
	var docs []string
	var cur []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimRight(l, " \t") == "---" {
			docs = append(docs, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		cur = append(cur, l)
	}
	return append(docs, strings.Join(cur, "\n"))
}

// yamlScalar reads a plain or quoted scalar after a key, without a trailing comment.
func yamlScalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
			return v[1 : 1+end]
		}
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// podsOfKubeFile lists the pods of the engine that `podman kube play` made from the resources: a Pod is the pod of its name, and a
// Deployment (or another workload) is the pod "<name>-pod" (with a number after it for each replica).
func (e *ext) podsOfKubeFile(rs []kubeResource) []containerapi.Pod {
	var out []containerapi.Pod
	for _, p := range e.pods {
		for _, r := range rs {
			if r.kind == "Pod" && p.Name == r.name || r.kind != "Pod" && r.kind != "Service" && (p.Name == r.name+"-pod" || strings.HasPrefix(p.Name, r.name+"-pod-")) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// maxKubeFile is the most bytes of a Kube file that are read.
const maxKubeFile = 1 << 20

// kubeDown is Kube Down: it asks first, naming the pods of the engine that the files made, and then runs `podman kube down`.
func (e *ext) kubeDown(paths []string) {
	if _, ok := e.ready(); !ok {
		return
	}
	folder, ok := e.needFolder("take Kube YAML down")
	if !ok {
		return
	}
	cli, err := e.findCLI()
	if err != nil {
		e.api.Notifications().Error("Cannot take Kube YAML down: " + e.cliMissingText())
		return
	}
	var pods []containerapi.Pod
	for _, p := range paths {
		text, err := readSmallFile(p)
		if err != nil {
			e.api.Notifications().Error("Cannot read " + relOrAbs(folder, p) + ": " + reasonOf(err))
			return
		}
		for _, pod := range e.podsOfKubeFile(parseKubeNames(text)) {
			if !containsPod(pods, pod.ID) {
				pods = append(pods, pod)
			}
		}
	}
	title := composeTitle(folder, paths)
	if len(pods) == 0 {
		e.api.Notifications().Info("No pod of " + title + " is in " + e.cfg.Engine + ".")
		return
	}
	n, running := 0, 0
	for _, p := range pods {
		n += len(p.Containers)
		if podRunning(p) {
			running++
		}
	}
	lines := []string{strings.Join(shortList(podNames(pods), 8), ", "), plural(n, "container") + " go with " + pluralWord(len(pods), "it", "them") + ". Volumes are kept."}
	if running > 0 {
		lines = append(lines, plural(running, "pod")+" running.")
	}
	run := func() {
		var jobs []cliJob
		for _, p := range paths {
			jobs = append(jobs, cliJob{argv: []string{cli, "kube", "down", relOrAbs(folder, p)}, dir: folder, env: e.cliEnviron(), name: e.cfg.cliName()})
		}
		e.logs.runCLI("Kube Down: "+title, jobs, func(res cliResult) {
			switch {
			case res.canceled:
			case res.err != nil:
				e.api.Notifications().Error("Cannot take " + title + " down: " + reasonOf(res.err))
			case !res.ok:
				e.api.Notifications().Error("Kube Down of " + title + " failed. The output is in the " + e.cfg.Name + " Logs tab.")
			default:
				e.api.Notifications().Info("Took " + title + " down.")
			}
		})
	}
	e.ask(confirmation{section: resPods, title: "Take " + plural(len(pods), "pod") + " down?", lines: lines,
		choices: []choice{{label: "Take down " + plural(len(pods), "pod"), run: run}}})
}

// containsPod tells whether the list has the pod.
func containsPod(ps []containerapi.Pod, id string) bool {
	for _, p := range ps {
		if p.ID == id {
			return true
		}
	}
	return false
}

// readSmallFile reads a file of at most maxKubeFile bytes. It fails with ContainerUIFileUnreadable.
func readSmallFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", errors.NewErr("ContainerUIFileUnreadable", err).SetMetadata("path", path)
	}
	if info.Size() > maxKubeFile {
		return "", errors.New("ContainerUIFileUnreadable", "the file is larger than 1 MiB", "path", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.NewErr("ContainerUIFileUnreadable", err).SetMetadata("path", path)
	}
	return string(data), nil
}

// kubeSubjectsOfContainers is the subject of Generate Kube YAML on containers.
func kubeSubjectsOfContainers(cs []containerapi.Container) kubeSubject {
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Name()
	}
	return kubeSubject{kind: "container", names: names}
}
