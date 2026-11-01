package java

import (
	"reflect"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The settings of the Java pack (SPEC §10.34 "As built (step 24b.6)"): the names, types and defaults of vscode-java's `java.*` settings that
// Eclipse JDT LS reads, with descriptions written for Emetos. The settings that decide what Emetos runs (java.jdt.ls.path,
// java.jdt.ls.java.home, java.jdt.ls.vmargs) and the ones that name a program or a file the server runs or reads from outside the folder
// (the Gradle and Maven locations, the JDKs) are read from the user's settings only. Every setting that differs from its default is sent
// to the server in initializationOptions.settings.java at its start, so a change of any of them restarts the server (SPEC §10.34 "As built
// (step 24b.1)").

const category = "Java"

// The settings that are not options of the server.
const (
	// PathSetting holds the jdtls folder (the folder that has plugins and config_linux).
	PathSetting = "java.jdt.ls.path"
	// JavaHomeSetting is the JDK that runs the server.
	JavaHomeSetting = "java.jdt.ls.java.home"
	// VMArgsSetting are the options of the JVM that runs the server.
	VMArgsSetting = "java.jdt.ls.vmargs"
	// TraceSetting traces the traffic with the server (the host's; vscode-java's name).
	TraceSetting = "java.trace.server"
	// UpdateBuildConfigurationSetting says what happens when a build file changes (disabled, interactive, automatic).
	UpdateBuildConfigurationSetting = "java.configuration.updateBuildConfiguration"
)

// defaultVMArgs are vscode-java's default options of the JVM (java.jdt.ls.vmargs).
const defaultVMArgs = "-XX:+UseParallelGC -XX:GCTimeRatio=4 -XX:AdaptiveSizePolicyWeight=90 -Dsun.zip.disableMemoryMapping=true -Xmx1G -Xms100m -Xlog:disable"

// option is a setting the server reads.
type option struct {
	key  string
	typ  string
	also []string
	def  any
	enum []any
	min  *float64
	desc string
	// machine says the setting is read from the user's settings only.
	machine bool
}

// strs makes a list of strings the way the settings read from JSON have them.
func strs(values ...string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// one is the minimum of the settings that count from one.
var one = 1.0

// boolOpt, strOpt and listOpt make options of the commonest forms.
func boolOpt(key string, def bool, desc string) option {
	return option{key: key, typ: "boolean", def: def, desc: desc}
}
func strOpt(key, def string, enum []any, desc string) option {
	return option{key: key, typ: "string", def: def, enum: enum, desc: desc}
}
func listOpt(key string, def []any, desc string) option {
	return option{key: key, typ: "array", def: def, desc: desc}
}

// options are the settings the server is given.
func options() []option {
	return []option{
		strOpt(UpdateBuildConfigurationSetting, "interactive", strs("disabled", "interactive", "automatic"),
			"What happens when pom.xml, build.gradle or another build file changes: nothing (disabled), the server asks whether to update the project (interactive), or it updates it (automatic)."),
		listOpt("java.import.exclusions", strs("**/node_modules/**", "**/.metadata/**", "**/archetype-resources/**", "**/META-INF/maven/**"),
			"Glob patterns of the folders the server does not look for projects in when it imports the folder."),
		listOpt("java.project.resourceFilters", strs("node_modules", "\\.git"), "Regular expressions of the files and folders the server leaves out of its projects."),
		strOpt("java.project.encoding", "ignore", strs("ignore", "warning", "setDefault"), "What to do about the encoding of the source files of a project: leave it, warn when it is not set, or set it to UTF-8."),
		listOpt("java.project.sourcePaths", []any{}, "The source folders of a project with no build file (an invisible project), relative to the folder."),
		{key: "java.project.outputPath", typ: "string", def: "", desc: "The output folder of a project with no build file, relative to the folder."},
		{key: "java.project.referencedLibraries", typ: "array", also: []string{"object"}, def: strs("lib/**/*.jar"),
			desc: "The jar files of a project with no build file that its classpath holds: glob patterns relative to the folder, or an object with include, exclude and sources."},

		boolOpt("java.import.maven.enabled", true, "Import Maven projects."),
		boolOpt("java.import.maven.offline.enabled", false, "Import Maven projects without the network: nothing is downloaded."),
		{key: "java.import.maven.projectNameTemplate", typ: "string", def: "", desc: "The template of the names of imported Maven projects ([groupId], [artifactId], [version])."},
		boolOpt("java.import.maven.disableTestClasspathFlag", false, "Do not use the test classpath flag when importing Maven projects."),
		boolOpt("java.maven.downloadSources", false, "Download the sources of the Maven dependencies, so that the definition of a library class opens its source."),
		boolOpt("java.maven.updateSnapshots", false, "Force the update of snapshots and releases when Maven resolves dependencies."),
		{key: "java.configuration.maven.userSettings", typ: "string", def: "", machine: true, desc: "The path of the user settings.xml of Maven. Only the user's settings are read."},
		{key: "java.configuration.maven.globalSettings", typ: "string", def: "", machine: true, desc: "The path of the global settings.xml of Maven. Only the user's settings are read."},
		strOpt("java.configuration.maven.notCoveredPluginExecutionSeverity", "warning", strs("ignore", "warning", "error"),
			"How serious it is when a Maven plugin execution is not covered by a lifecycle mapping."),
		strOpt("java.configuration.maven.defaultMojoExecutionAction", "ignore", strs("ignore", "warn", "error", "execute"),
			"What to do with the executions of Maven plugins that have no lifecycle mapping."),

		boolOpt("java.import.gradle.enabled", true, "Import Gradle projects."),
		boolOpt("java.import.gradle.wrapper.enabled", true, "Use the Gradle wrapper of the project (gradlew) when it has one. The wrapper downloads Gradle when it is not there yet."),
		{key: "java.import.gradle.version", typ: "string", def: "", desc: "The Gradle version to use when the project has no wrapper."},
		{key: "java.import.gradle.home", typ: "string", def: "", machine: true, desc: "The folder of a Gradle installation to use when the project has no wrapper. Only the user's settings are read."},
		{key: "java.import.gradle.java.home", typ: "string", def: "", machine: true, desc: "The JDK that runs Gradle. Only the user's settings are read."},
		{key: "java.import.gradle.user.home", typ: "string", def: "", machine: true, desc: "The Gradle user home folder (GRADLE_USER_HOME). Only the user's settings are read."},
		boolOpt("java.import.gradle.offline.enabled", false, "Import Gradle projects without the network."),
		{key: "java.import.gradle.arguments", typ: "string", def: "", machine: true, desc: "Arguments for the Gradle command that imports a project. Only the user's settings are read."},
		{key: "java.import.gradle.jvmArguments", typ: "string", def: "", machine: true, desc: "JVM arguments for the Gradle command that imports a project. Only the user's settings are read."},
		boolOpt("java.import.gradle.annotationProcessing.enabled", true, "Run the annotation processors of Gradle projects."),

		boolOpt("java.autobuild.enabled", true, "Build the projects automatically when a file changes."),
		{key: "java.maxConcurrentBuilds", typ: "integer", def: float64(1), min: &one, desc: "How many builds may run at once."},
		listOpt("java.compile.nullAnalysis.nonnull", strs("javax.annotation.Nonnull", "org.eclipse.jdt.annotation.NonNull", "org.springframework.lang.NonNull", "org.jspecify.annotations.NonNull"),
			"The annotations that mean a value is never null."),
		listOpt("java.compile.nullAnalysis.nullable", strs("javax.annotation.Nullable", "org.eclipse.jdt.annotation.Nullable", "org.springframework.lang.Nullable", "org.jspecify.annotations.Nullable"),
			"The annotations that mean a value may be null."),
		listOpt("java.compile.nullAnalysis.nonnullbydefault", strs("javax.annotation.ParametersAreNonnullByDefault", "org.eclipse.jdt.annotation.NonNullByDefault", "org.springframework.lang.NonNullApi", "org.jspecify.annotations.NullMarked"),
			"The annotations that make the values of a package or class non-null by default."),
		strOpt("java.compile.nullAnalysis.mode", "interactive", strs("disabled", "interactive", "automatic"), "Whether the null annotations of the project are analyzed: never, when the server asks, or always."),
		strOpt("java.errors.incompleteClasspath.severity", "warning", strs("ignore", "info", "warning", "error"), "How serious it is when the classpath of a project is incomplete."),
		{key: "java.configuration.runtimes", typ: "array", def: []any{}, machine: true,
			desc: "The JDKs a project can be compiled against: a list of objects with name (JavaSE-21), path, and default (true for the one of projects that name none). Only the user's settings are read."},

		boolOpt("java.format.enabled", true, "Format Java files with the server."),
		{key: "java.format.settings.url", typ: "string", def: "", desc: "The Eclipse formatter profile (an XML file or its address) the server formats with."},
		{key: "java.format.settings.profile", typ: "string", def: "", desc: "The name of the profile of the Eclipse formatter file that is used."},
		boolOpt("java.format.comments.enabled", true, "Format the comments too."),
		boolOpt("java.format.onType.enabled", true, "Format as you type: after a semicolon, a closing brace and a new line."),

		boolOpt("java.completion.enabled", true, "Offer completions."),
		boolOpt("java.completion.postfix.enabled", true, "Offer postfix completions (list.for, value.var)."),
		boolOpt("java.completion.chain.enabled", false, "Offer chains of calls and fields that lead to the type that is needed."),
		listOpt("java.completion.favoriteStaticMembers", strs("org.junit.Assert.*", "org.junit.Assume.*", "org.junit.jupiter.api.Assertions.*", "org.junit.jupiter.api.Assumptions.*",
			"org.junit.jupiter.api.DynamicContainer.*", "org.junit.jupiter.api.DynamicTest.*", "org.mockito.Mockito.*", "org.mockito.ArgumentMatchers.*", "org.mockito.Answers.*"),
			"Static members that are offered in completions without their class having been imported."),
		listOpt("java.completion.filteredTypes", strs("java.awt.*", "com.sun.*", "sun.*", "jdk.*", "org.graalvm.*", "io.micrometer.shaded.*"), "Types and packages that completions and imports never offer."),
		strOpt("java.completion.guessMethodArguments", "auto", strs("auto", "off", "insertParameterNames", "insertBestGuessedArguments"),
			"What completing a method writes between its parentheses: the names of the parameters, the arguments the server guesses, or nothing."),
		strOpt("java.completion.matchCase", "firstLetter", strs("firstLetter", "off"), "Whether the case of the first letter must match the typed text."),
		listOpt("java.completion.importOrder", strs("#", "java", "javax", "org", "com", ""), "The order of the imports: package prefixes, # for the static imports and an empty text for the rest."),
		boolOpt("java.completion.lazyResolveTextEdit.enabled", true, "Work out the edit of a completion item only when it is chosen."),
		{key: "java.completion.maxResults", typ: "integer", def: float64(0), desc: "The most completion items the server answers with; 0 for no limit."},
		boolOpt("java.completion.collapseCompletionItems", false, "Collapse the overloads of a method into one completion item."),
		boolOpt("java.signatureHelp.enabled", true, "Show the signature of a method while its arguments are typed."),
		boolOpt("java.signatureHelp.description.enabled", false, "Show the documentation of the method in the signature help."),

		strOpt("java.codeGeneration.insertionLocation", "afterCursor", strs("afterCursor", "beforeCursor", "lastMember"), "Where generated members (getters, constructors, toString) are inserted."),
		strOpt("java.codeGeneration.addFinalForNewDeclaration", "none", strs("none", "fields", "variables", "all"), "Add final to the fields and variables that code generation declares."),
		boolOpt("java.codeGeneration.hashCodeEquals.useJava7Objects", false, "Generate hashCode and equals with java.util.Objects."),
		boolOpt("java.codeGeneration.hashCodeEquals.useInstanceof", false, "Generate equals with instanceof instead of comparing the classes."),
		boolOpt("java.codeGeneration.useBlocks", false, "Put the bodies of generated if, for and while statements in braces."),
		boolOpt("java.codeGeneration.generateComments", false, "Generate comments for the code that is generated."),
		{key: "java.codeGeneration.toString.template", typ: "string", def: "${object.className} [${member.name()}=${member.value}, ${otherMembers}]", desc: "The template of the generated toString method."},
		strOpt("java.codeGeneration.toString.codeStyle", "STRING_CONCATENATION", strs("STRING_CONCATENATION", "STRING_BUILDER", "STRING_BUILDER_CHAINED", "STRING_FORMAT"), "The code style of the generated toString method."),
		boolOpt("java.codeGeneration.toString.skipNullValues", false, "Leave the fields that are null out of the generated toString."),
		boolOpt("java.codeGeneration.toString.listArrayContents", true, "List the contents of arrays in the generated toString."),
		{key: "java.codeGeneration.toString.limitElements", typ: "integer", def: float64(0), desc: "The most elements of an array or collection the generated toString lists; 0 for no limit."},
		boolOpt("java.edit.smartSemicolonDetection.enabled", false, "Put the semicolon where it belongs when it is typed in the middle of a line."),

		listOpt("java.cleanup.actions", strs("renameFileToType"), "The clean ups that the command Clean Up runs."),
		listOpt("java.cleanup.actionsOnSave", []any{}, "The clean ups that run when a file is saved."),
		boolOpt("java.saveActions.organizeImports", false, "Organize the imports when a file is saved."),
		{key: "java.sources.organizeImports.starThreshold", typ: "integer", def: float64(99), min: &one, desc: "How many imports from one package are folded into an import with a star."},
		{key: "java.sources.organizeImports.staticStarThreshold", typ: "integer", def: float64(99), min: &one, desc: "How many static imports from one class are folded into an import with a star."},
		strOpt("java.quickfix.showAt", "line", strs("line", "problem"), "Where the light bulb of the quick fixes is shown: on the line or on the problem."),

		boolOpt("java.hover.javadoc.enabled", true, "Show the Javadoc of the symbol in the hover."),
		boolOpt("java.referencesCodeLens.enabled", false, "Show the number of references above the declarations."),
		boolOpt("java.referencesCodeLens.includeFields", false, "Show the number of references above fields too."),
		strOpt("java.implementationCodeLens", "none", strs("none", "types", "methods", "all"), "Show the number of implementations above types, methods or both."),
		boolOpt("java.references.includeAccessors", true, "Find the getters and setters of a field when its references are asked for."),
		boolOpt("java.references.includeDeclarations", true, "Include the declaration of the symbol in its references."),
		boolOpt("java.references.includeDecompiledSources", true, "Search the decompiled class files of the libraries too."),
		boolOpt("java.symbols.includeSourceMethodDeclarations", false, "Include the methods of the source files in the symbols of the workspace."),
		boolOpt("java.symbols.includeGeneratedCode", false, "Include the code that annotation processors and Lombok generate in the symbols."),
		strOpt("java.inlayHints.parameterNames.enabled", "literals", strs("none", "literals", "all"), "Show the names of the parameters in a call: none, only before literals, or before every argument."),
		boolOpt("java.inlayHints.parameterNames.suppressWhenSameNameNumbered", true, "Leave out the name of a parameter when the argument has the same name with a number."),
		listOpt("java.inlayHints.parameterNames.exclusions", []any{}, "Methods whose parameter names are never shown: patterns such as java.util.List.add."),
		boolOpt("java.inlayHints.variableTypes.enabled", false, "Show the type after a variable declared with var."),
		boolOpt("java.inlayHints.parameterTypes.enabled", false, "Show the types of the parameters of a lambda."),
		boolOpt("java.inlayHints.formatParameters.enabled", false, "Format the parameters of a call in the hints."),
		strOpt("java.search.scope", "all", strs("all", "main", "projectOnly"), "The scope of the searches: everything, only the main sources, or only the project's (not its libraries)."),
		boolOpt("java.eclipse.downloadSources", false, "Download the sources of the libraries of Eclipse projects."),
		boolOpt("java.foldingRange.enabled", true, "Fold with the server's ranges."),
		boolOpt("java.selectionRange.enabled", true, "Expand the selection with the server's ranges."),
		listOpt("java.diagnostic.filter", []any{}, "Glob patterns of the files whose problems are not shown."),
	}
}

// settingSpecs are the settings the manifest declares besides the path setting of the server (langserver.Config.SettingSpecs).
func settingSpecs() []extension.SettingSpec {
	specs := []extension.SettingSpec{
		{
			Key: JavaHomeSetting, Type: "string", Default: "", Category: category, Scope: "machine",
			Description: "The JDK that runs the language server: a folder that has bin/java, Java 21 or newer. When empty, JAVA_HOME, then java in PATH. Only the user's settings are read.",
		},
		{
			Key: VMArgsSetting, Type: "string", Default: defaultVMArgs, Category: category, Scope: "machine",
			Description: "The options of the JVM that runs the language server (memory, garbage collector, -D properties), separated by spaces; quote an option that holds spaces. Only the user's settings are read.",
		},
	}
	for _, o := range options() {
		spec := extension.SettingSpec{Key: o.key, Type: o.typ, Default: o.def, Enum: o.enum, Minimum: o.min, AlsoTypes: o.also, Category: category, Description: o.desc}
		if o.machine {
			spec.Scope = "machine"
		}
		specs = append(specs, spec)
	}
	return specs
}

// javaSettings reads every setting the server is given, so that a change of any of them restarts it, and returns what differs from its
// default as the nested object the server reads: {"import": {"maven": {"enabled": false}}}.
func javaSettings(s *langserver.Settings) map[string]any {
	out := map[string]any{}
	for _, o := range options() {
		v, ok := s.Value(o.key)
		if !ok || reflect.DeepEqual(normalize(v), normalize(o.def)) {
			continue
		}
		if str, isString := v.(string); isString && strings.TrimSpace(str) == "" {
			continue // an empty path or name is a setting that is not set
		}
		langserver.SetNested(out, strings.Split(strings.TrimPrefix(o.key, "java."), "."), v)
	}
	return out
}

// normalize makes a value comparable with another read from JSON: a nil array and an empty one are the same default.
func normalize(v any) any {
	if list, ok := v.([]any); ok && len(list) == 0 {
		return []any{}
	}
	return v
}
