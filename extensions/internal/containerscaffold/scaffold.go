// Package containerscaffold makes the files "Add Docker Files to Workspace" writes (SPEC §10.20 "Scaffolding"): a Dockerfile (or
// Containerfile), a .dockerignore and, when asked, a compose.yaml, for a Go, Node.js, Python or .NET project or a plain image. It
// only produces the text (Generate) and reads the few facts it needs from the project (package.json, a project file, the entry file
// of a Python app); the extension asks the user and writes the files.
//
// Ported from vscode-containers (MIT): extensions/vscode-containers/resources/templates/ (.dockerignore, compose.yaml, go/Dockerfile,
// node/Dockerfile, python/Dockerfile and requirements.txt, netCore/Dockerfile and other/Dockerfile: the Handlebars templates written
// again as Go templates, with the same text) and src/scaffolding/wizard/ (the information each platform gathers: the start command of a
// Node.js package, the command of a Python app by its framework, the base images of a .NET project). Not built: Java, C++ and Ruby, the
// Windows images of .NET, and compose.debug.yaml with the debugging configuration (Emetos has no debug adapter for containers).
package containerscaffold

import (
	"embed"
	"encoding/json"
	"encoding/xml"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/rm4n0s/errors"
)

//go:embed templates
var templates embed.FS

// Platform is the kind of application a Dockerfile is made for.
type Platform string

// The platforms (the names of Container Tools' list).
const (
	NodeJS      Platform = "Node.js"
	NetCore     Platform = "ASP.NET Core"
	NetConsole  Platform = ".NET: Console"
	Django      Platform = "Python: Django"
	FastAPI     Platform = "Python: FastAPI"
	Flask       Platform = "Python: Flask"
	PythonOther Platform = "Python: General"
	Go          Platform = "Go"
	Other       Platform = "Other"
)

// Platforms lists the platforms in the order of the choice.
var Platforms = []Platform{NodeJS, NetCore, NetConsole, Django, FastAPI, Flask, PythonOther, Go, Other}

// IsPython tells whether the platform is one of the Python ones.
func (p Platform) IsPython() bool {
	return p == Django || p == FastAPI || p == Flask || p == PythonOther
}

// IsNet tells whether the platform is one of the .NET ones.
func (p Platform) IsNet() bool { return p == NetCore || p == NetConsole }

// DefaultPorts are the ports a platform listens on unless the user says otherwise (the default of the question): none for those
// that listen on nothing in particular.
func (p Platform) DefaultPorts() []int {
	switch p {
	case NodeJS, Go:
		return []int{3000}
	case Django, FastAPI:
		return []int{8000}
	case Flask:
		return []int{5000}
	case NetCore:
		return []int{5000}
	}
	return nil
}

// AsksPorts tells whether the question about the ports is asked for the platform.
func (p Platform) AsksPorts() bool {
	return p == NodeJS || p == Go || p == Django || p == FastAPI || p == Flask || p == NetCore
}

// Artifact is what a platform needs to know about the project.
type Artifact int

// The kinds of artifact a platform needs.
const (
	// NoArtifact is a platform that needs none (Go, Other).
	NoArtifact Artifact = iota
	// PackageJSON is the package.json of Node.js.
	PackageJSON
	// ProjectFile is the .csproj or .fsproj of .NET.
	ProjectFile
	// PythonEntry is the file (or the module) that starts a Python app.
	PythonEntry
)

// ArtifactKind is the artifact the platform needs.
func (p Platform) ArtifactKind() Artifact {
	switch {
	case p == NodeJS:
		return PackageJSON
	case p.IsNet():
		return ProjectFile
	case p.IsPython():
		return PythonEntry
	}
	return NoArtifact
}

// Input is what the user chose.
type Input struct {
	// Platform is the kind of application.
	Platform Platform
	// Folder is the folder of the workspace, absolute.
	Folder string
	// Artifact is the package.json, the project file or the entry file of the Python app, relative to Folder with slashes; Module is a
	// Python module (myapp.manage) instead of the entry file.
	Artifact, Module string
	// Ports are the ports the app listens on.
	Ports []int
	// Compose adds a compose.yaml.
	Compose bool
	// FileName is the name of the file that describes the image: Dockerfile (the default) or Containerfile.
	FileName string
	// ReadFile reads a file, absolute path; nil is os.ReadFile. Glob lists the files that match a pattern in a folder
	// (filepath.Glob); nil is filepath.Glob. Tests give their own.
	ReadFile func(path string) ([]byte, error)
	Glob     func(pattern string) ([]string, error)
}

// File is a file to write.
type File struct {
	// Path is relative to the folder of the workspace, with slashes.
	Path string
	// Content is the text of the file.
	Content string
	// KeepExisting says that the file is left alone when it exists (requirements.txt, which holds the user's own packages); any other
	// file that exists with other content is asked about.
	KeepExisting bool
}

// context is what the templates read.
type context struct {
	// files holds the templates (the embedded ones; a test gives others).
	files                       fs.ReadFileFS
	Platform                    Platform
	ServiceName, Version        string
	Ports                       []int
	WorkspaceDir                string
	BuildContext, DockerfileDir string
	DockerfileName              string
	Artifact                    string
	PythonProjectType           string
	NodeCmdParts                []string
	PythonCmdParts              []string
	WsgiComment                 string
	NetCoreAssemblyName         string
	NetCoreRuntimeBaseImage     string
	NetCoreSdkBaseImage         string
	NetCoreBaseImageDefaultUser string
}

// Generate makes the files for the choices. It fails with ContainerScaffoldPlatformUnknown, ContainerScaffoldArtifactMissing (a
// platform that needs an artifact and has none), ContainerScaffoldArtifactInvalid (a package.json or project file that cannot be read
// or understood) and ContainerScaffoldPortsInvalid.
func Generate(in Input) ([]File, error) {
	if !slices.Contains(Platforms, in.Platform) {
		return nil, errors.New("ContainerScaffoldPlatformUnknown", "this is not a platform to make a Dockerfile for", "platform", string(in.Platform))
	}
	for _, p := range in.Ports {
		if p < 1 || p > 65535 {
			return nil, errors.New("ContainerScaffoldPortsInvalid", "a port is a number from 1 to 65535", "port", strconv.Itoa(p))
		}
	}
	name := in.FileName
	if name == "" {
		name = "Dockerfile"
	}
	c := &context{
		files: templates, Platform: in.Platform, Version: "0.0.1", Ports: in.Ports, WorkspaceDir: in.Folder, DockerfileName: name,
		BuildContext: in.Folder, DockerfileDir: in.Folder, ServiceName: ImageName(filepath.Base(in.Folder)),
	}
	if kind := in.Platform.ArtifactKind(); kind != NoArtifact && in.Artifact == "" && in.Module == "" {
		return nil, errors.New("ContainerScaffoldArtifactMissing", "this platform needs the file that starts the app", "platform", string(in.Platform))
	}
	if in.Artifact != "" {
		c.Artifact = filepath.Join(in.Folder, filepath.FromSlash(in.Artifact))
	}
	var err error
	switch {
	case in.Platform == NodeJS:
		err = c.gatherNode(in)
	case in.Platform.IsNet():
		err = c.gatherNet(in)
	case in.Platform.IsPython():
		err = c.gatherPython(in)
	}
	if err != nil {
		return nil, err
	}

	dir := templateDir(in.Platform)
	var files []File
	render := func(file, tmpl string, keep bool) error {
		text, err := c.render(tmpl)
		if err != nil {
			return err
		}
		files = append(files, File{Path: file, Content: text, KeepExisting: keep})
		return nil
	}
	rel := func(dir string) string {
		r, err := filepath.Rel(in.Folder, dir)
		if err != nil || r == "." {
			return ""
		}
		return filepath.ToSlash(r) + "/"
	}
	if err := render(rel(c.BuildContext)+".dockerignore", "dockerignore.tmpl", false); err != nil {
		return nil, err
	}
	if err := render(rel(c.DockerfileDir)+name, dir+"/Dockerfile.tmpl", false); err != nil {
		return nil, err
	}
	if in.Platform.IsPython() {
		if err := render("requirements.txt", "python/requirements.txt.tmpl", true); err != nil {
			return nil, err
		}
	}
	if in.Compose {
		if err := render("compose.yaml", "compose.tmpl", false); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// templateDir is the folder of the templates of a platform.
func templateDir(p Platform) string {
	switch {
	case p == NodeJS:
		return "node"
	case p.IsNet():
		return "netcore"
	case p.IsPython():
		return "python"
	case p == Go:
		return "go"
	}
	return "other"
}

// ImageName makes a valid image name of a hint: lowercase letters and digits (Container Tools' getValidImageName).
func ImageName(hint string) string {
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

// ParsePorts reads a comma-separated list of ports; an empty text is no port. It fails with ContainerScaffoldPortsInvalid.
func ParsePorts(text string) ([]int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	var out []int
	for _, part := range strings.Split(text, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("ContainerScaffoldPortsInvalid", "ports are a comma-separated list of numbers from 1 to 65535, or empty for no exposed port", "ports", text)
		}
		out = append(out, n)
	}
	return out, nil
}

// funcs are the helpers of the templates (the Handlebars helpers of Container Tools).
func (c *context) funcs() template.FuncMap {
	norm := func(p string) string {
		p = filepath.ToSlash(p)
		if p == "" {
			return "."
		}
		return path.Clean(p)
	}
	rel := func(base, target string) string {
		r, err := filepath.Rel(base, target)
		if err != nil {
			return norm(target)
		}
		return norm(r)
	}
	return template.FuncMap{
		"workspaceRelative": func(p string) string { return rel(c.WorkspaceDir, p) },
		"contextRelative":   func(p string) string { return rel(c.BuildContext, p) },
		"basename":          func(p string) string { return filepath.Base(p) },
		"dirname":           func(p string) string { return norm(filepath.Dir(p)) },
		"toQuotedArray": func(parts []string) string {
			q := make([]string, len(parts))
			for i, p := range parts {
				q[i] = `"` + p + `"`
			}
			return "[" + strings.Join(q, ", ") + "]"
		},
		"isRootPort": func(ports []int) bool {
			for _, p := range ports {
				if p < 1024 {
					return true
				}
			}
			return false
		},
	}
}

// render fills a template.
func (c *context) render(name string) (string, error) {
	text, err := c.files.ReadFile("templates/" + name)
	if err != nil {
		return "", errors.NewErr("ContainerScaffoldTemplateMissing", err).SetMetadata("template", name)
	}
	t, err := template.New(name).Funcs(c.funcs()).Parse(string(text))
	if err != nil {
		return "", errors.NewErr("ContainerScaffoldTemplateInvalid", err).SetMetadata("template", name)
	}
	var b strings.Builder
	if err := t.Execute(&b, c); err != nil {
		return "", errors.NewErr("ContainerScaffoldTemplateInvalid", err).SetMetadata("template", name)
	}
	return b.String(), nil
}

// ---- what each platform gathers ----

// readFile reads a file of the project.
func (in Input) readFile(p string) ([]byte, error) {
	if in.ReadFile != nil {
		return in.ReadFile(p)
	}
	return readOS(p)
}

// gatherNode reads package.json: the start command, the name and the version. The Dockerfile and the context are next to it.
func (c *context) gatherNode(in Input) error {
	data, err := in.readFile(c.Artifact)
	if err != nil {
		return errors.NewErr("ContainerScaffoldArtifactInvalid", err).SetMetadata("file", in.Artifact)
	}
	var pkg struct {
		Name, Version, Main string
		Scripts             map[string]string
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return errors.NewErr("ContainerScaffoldArtifactInvalid", err).SetMetadata("file", in.Artifact)
	}
	switch {
	case pkg.Scripts["start"] != "":
		c.NodeCmdParts = []string{"npm", "start"}
	case pkg.Main != "":
		c.NodeCmdParts = []string{"node", pkg.Main}
	default:
		c.NodeCmdParts = []string{"npm", "start"}
	}
	if pkg.Version != "" {
		c.Version = pkg.Version
	}
	if pkg.Name != "" {
		c.ServiceName = ImageName(pkg.Name)
	}
	c.BuildContext = filepath.Dir(c.Artifact)
	c.DockerfileDir = c.BuildContext
	return nil
}

// netVersion finds the major and minor version of the target framework (net8.0, netcoreapp3.1).
var netVersion = regexp.MustCompile(`(?i)net(?:coreapp)?(\d+)\.(\d+)`)

// gatherNet reads the project file: the assembly name and the target framework, which give the images. The Dockerfile is next to the
// project file, the context is the folder of the workspace.
func (c *context) gatherNet(in Input) error {
	data, err := in.readFile(c.Artifact)
	if err != nil {
		return errors.NewErr("ContainerScaffoldArtifactInvalid", err).SetMetadata("file", in.Artifact)
	}
	var proj struct {
		Groups []struct {
			AssemblyName     string `xml:"AssemblyName"`
			TargetFramework  string `xml:"TargetFramework"`
			TargetFrameworks string `xml:"TargetFrameworks"`
		} `xml:"PropertyGroup"`
	}
	if err := xml.Unmarshal(data, &proj); err != nil {
		return errors.NewErr("ContainerScaffoldArtifactInvalid", err).SetMetadata("file", in.Artifact)
	}
	var assembly, framework string
	for _, g := range proj.Groups {
		if assembly == "" {
			assembly = strings.TrimSpace(g.AssemblyName)
		}
		if framework == "" {
			framework = strings.TrimSpace(g.TargetFramework)
		}
		if framework == "" {
			framework, _, _ = strings.Cut(strings.TrimSpace(g.TargetFrameworks), ";")
		}
	}
	m := netVersion.FindStringSubmatch(framework)
	if m == nil {
		return errors.New("ContainerScaffoldArtifactInvalid", "the target framework of the project file is not known (net8.0 is the form)", "file", in.Artifact, "framework", framework)
	}
	major, _ := strconv.Atoi(m[1])
	version := m[1] + "." + m[2]
	base := "mcr.microsoft.com/dotnet/runtime"
	if c.Platform == NetCore {
		base = "mcr.microsoft.com/dotnet/aspnet"
	}
	c.NetCoreRuntimeBaseImage = base + ":" + version
	c.NetCoreSdkBaseImage = "mcr.microsoft.com/dotnet/sdk:" + version
	if major >= 8 {
		c.NetCoreBaseImageDefaultUser = "app"
	}
	if assembly == "" {
		assembly = strings.TrimSuffix(filepath.Base(c.Artifact), filepath.Ext(c.Artifact))
	}
	c.NetCoreAssemblyName = assembly + ".dll"
	c.ServiceName = ImageName(strings.TrimSuffix(filepath.Base(c.Artifact), filepath.Ext(c.Artifact)))
	c.DockerfileDir = filepath.Dir(c.Artifact)
	return nil
}

// The ports the frameworks of Python listen on unless told otherwise.
const (
	defaultDjangoPort = 8000
	defaultFlaskPort  = 5000
)

// gatherPython makes the command that starts the app, by its framework (Container Tools' PythonGatherInformationStep).
func (c *context) gatherPython(in Input) error {
	port := func(def int) int {
		if len(c.Ports) > 0 {
			return c.Ports[0]
		}
		return def
	}
	module := in.Module
	file := in.Artifact
	if module == "" {
		module = strings.ReplaceAll(strings.TrimSuffix(file, path.Ext(file)), "/", ".")
	}
	app := []string{file}
	if in.Module != "" {
		app = []string{"-m", in.Module}
	}
	switch c.Platform {
	case Django:
		c.PythonProjectType = "django"
		wsgi := ""
		if in.Artifact != "" {
			glob := in.Glob
			if glob == nil {
				glob = filepath.Glob
			}
			dir := filepath.Dir(c.Artifact)
			for _, pat := range []string{"wsgi.py", "WSGI.py", "Wsgi.py"} {
				if m, _ := glob(filepath.Join(dir, "*", pat)); len(m) > 0 {
					wsgi = filepath.Base(filepath.Dir(m[0])) + ".wsgi"
					break
				}
			}
		}
		if wsgi == "" {
			c.WsgiComment = "# File wsgi.py was not found. Please enter the Python path to wsgi file."
			wsgi = "pythonPath.to.wsgi"
		}
		c.PythonCmdParts = []string{"gunicorn", "--bind", "0.0.0.0:" + strconv.Itoa(port(defaultDjangoPort)), wsgi}
	case FastAPI:
		c.PythonProjectType = "fastapi"
		c.PythonCmdParts = []string{"gunicorn", "--bind", "0.0.0.0:" + strconv.Itoa(port(defaultDjangoPort)), "-k", "uvicorn.workers.UvicornWorker", module + ":app"}
	case Flask:
		c.PythonProjectType = "flask"
		c.PythonCmdParts = []string{"gunicorn", "--bind", "0.0.0.0:" + strconv.Itoa(port(defaultFlaskPort)), module + ":app"}
	default:
		c.PythonProjectType = "general"
		c.PythonCmdParts = append([]string{"python"}, app...)
	}
	return nil
}
