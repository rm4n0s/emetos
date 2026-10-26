package golang

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// "Go: Add Tags To Struct Fields" and "Go: Remove Tags From Struct Fields" (SPEC §10.17 "As built (step 14.2)"): the settings
// go.addTags and go.removeTags of the Go extension of VS Code, the prompts, and the work done by gopls (its command
// gopls.modify_tags) when the server has it, else by the editor of tags in tags.go on the text of the document.

// The ids of the settings and the commands of the tags.
const (
	// AddTagsSetting and RemoveTagsSetting are go.addTags and go.removeTags, objects with the members of tagSettings.
	AddTagsSetting    = "go.addTags"
	RemoveTagsSetting = "go.removeTags"
	// AddTagsCommand and RemoveTagsCommand are the commands.
	AddTagsCommand    = ID + ".addTags"
	RemoveTagsCommand = ID + ".removeTags"
	// modifyTagsCommand is the command of gopls.
	modifyTagsCommand = "gopls.modify_tags"
)

// tagSettings are the members of go.addTags and go.removeTags (Transform and Template are for adding only).
type tagSettings struct {
	PromptForTags bool   `json:"promptForTags"`
	Tags          string `json:"tags"`
	Options       string `json:"options"`
	Transform     string `json:"transform"`
	Template      string `json:"template"`
}

// tagConfig reads the setting and lets the arguments of the command replace its members, as VS Code's Go extension does: a
// member the arguments name wins.
func (g *golang) tagConfig(key string, args any) tagSettings {
	cfg, err := extension.SettingValue[tagSettings](g.api.Settings(), key)
	if err != nil {
		g.api.Log().Warn("the setting of the tags cannot be read", "key", key, "err", err)
	}
	m, _ := args.(map[string]any)
	if v, ok := m["promptForTags"].(bool); ok {
		cfg.PromptForTags = v
	}
	for name, dst := range map[string]*string{"tags": &cfg.Tags, "options": &cfg.Options, "transform": &cfg.Transform, "template": &cfg.Template} {
		if v, ok := m[name].(string); ok {
			*dst = v
		}
	}
	return cfg
}

// tagRequest is what the user asked for, after the prompts.
type tagRequest struct {
	remove                           bool
	tags, options, transform, format string
}

// addTagsCommand is the command "Go: Add Tags To Struct Fields".
func (g *golang) addTagsCommand(ctx context.Context, args any) error {
	return g.tagsCommand(ctx, false, args)
}

// removeTagsCommand is the command "Go: Remove Tags From Struct Fields".
func (g *golang) removeTagsCommand(ctx context.Context, args any) error {
	return g.tagsCommand(ctx, true, args)
}

// tagsCommand finds the file and the selection, asks what is missing and changes the tags.
func (g *golang) tagsCommand(ctx context.Context, remove bool, args any) error {
	what, key := "Add Tags", AddTagsSetting
	if remove {
		what, key = "Remove Tags", RemoveTagsSetting
	}
	file, ok := g.activeGoFile(ctx, what)
	if !ok {
		return nil
	}
	if !g.editor.hasSel {
		g.api.Notifications().Info(what + ": click in the struct first.")
		return nil
	}
	cfg := g.tagConfig(key, args)
	req := tagRequest{remove: remove, tags: cfg.Tags, options: cfg.Options, transform: cfg.Transform, format: cfg.Template}
	// the text and the selection are read again after the prompts: the user may have typed meanwhile
	run := func() { g.modifyTags(ctx, what, g.refreshed(ctx, file), req) }
	// the user is asked when the setting says so, or when it holds no tags and no options
	if !cfg.PromptForTags && (cfg.Tags != "" || cfg.Options != "") {
		run()
		return nil
	}
	return g.promptTags(what, &req, run)
}

// promptTags asks for the tags and the options, and for the transform when adding, the way VS Code's Go extension does: two input
// boxes with the configured values in them, then a list. Closing any of them ends the command. The answers are written to req,
// and run is called after the last one.
func (g *golang) promptTags(what string, req *tagRequest, run func()) error {
	ask := func(prompt, value string, next func(string)) error {
		return g.api.InputBox().Show(extension.InputBoxOptions{Prompt: prompt, Value: value}, func(text string, ok bool) {
			if ok {
				next(strings.TrimSpace(text))
			}
		})
	}
	return ask("Enter comma separated tag names", req.tags, func(tags string) {
		req.tags = tags
		err := ask("Enter comma separated options (tag=option)", req.options, func(options string) {
			req.options = options
			if req.remove {
				run()
				return
			}
			g.pickTransform(what, req, run)
		})
		if err != nil {
			g.api.Notifications().Error(what + ": " + describe(err))
		}
	})
}

// pickTransform asks how the names of the fields become the names in the tags.
func (g *golang) pickTransform(what string, req *tagRequest, run func()) {
	names := []string{"snakecase", "camelcase", "lispcase", "pascalcase", "keep"}
	items := make([]extension.QuickPickItem, len(names))
	for i, n := range names {
		items[i] = extension.QuickPickItem{Label: n}
		if n == req.transform || (req.transform == "" && n == "snakecase") {
			items[i].Description = "default"
		}
	}
	err := g.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select transform", Items: items}, func(i int) {
		if i >= 0 {
			req.transform = names[i]
			run()
		}
	})
	if err != nil {
		g.api.Notifications().Error(what + ": " + describe(err))
	}
}

// modification is the request as the editor of tags takes it.
func (r tagRequest) modification() (tagMod, error) {
	if r.remove {
		if r.tags == "" && r.options == "" {
			return tagMod{Clear: true, ClearOptions: true}, nil
		}
		opts, err := parseOptions(r.options)
		if err != nil {
			return tagMod{}, err
		}
		return tagMod{Remove: splitList(r.tags), RemoveOptions: opts}, nil
	}
	opts, err := parseOptions(r.options)
	if err != nil {
		return tagMod{}, err
	}
	return tagMod{Add: splitList(r.tags), AddOptions: opts, Transform: r.transform, ValueFormat: r.format}, nil
}

// modifyTags changes the tags: with gopls when it is running and has the command, else here.
func (g *golang) modifyTags(ctx context.Context, what string, file goFile, req tagRequest) {
	if !req.remove && req.tags == "" && req.options == "" {
		g.api.Notifications().Info(what + ": no tags or options were given, so nothing was added.")
		return
	}
	mod, err := req.modification()
	if err == nil {
		err = mod.validate()
	}
	if err != nil {
		g.api.Notifications().Warn(what + ": " + describe(err))
		return
	}
	if commands, cerr := g.api.LanguageServers().Commands(ServerID); cerr == nil && contains(commands, modifyTagsCommand) {
		g.modifyTagsWithGopls(ctx, what, file, req, mod)
		return
	}
	g.modifyTagsHere(ctx, what, file, mod)
}

// modifyTagsWithGopls asks gopls to change the tags; it applies the edit itself, as an edit of the window.
func (g *golang) modifyTagsWithGopls(ctx context.Context, what string, file goFile, req tagRequest, mod tagMod) {
	lsp := func(p extension.Position) map[string]any {
		return map[string]any{"line": p.Line, "character": utf16Column(file.text, p)}
	}
	start, end := positionAt(file.text, file.start), positionAt(file.text, file.end)
	args := map[string]any{
		"URI": file.uri, "Range": map[string]any{"start": lsp(start), "end": lsp(end)},
		"Transform": mod.Transform, "ValueFormat": mod.ValueFormat,
	}
	if req.remove {
		args["Modification"] = "remove"
		args["Remove"] = strings.Join(mod.Remove, ",")
		args["RemoveOptions"] = req.options
		args["Clear"], args["ClearOptions"] = mod.Clear, mod.ClearOptions
	} else {
		args["Modification"] = "add"
		args["Add"] = strings.Join(mod.Add, ",")
		args["AddOptions"] = req.options
	}
	err := g.api.LanguageServers().ExecuteCommand(ctx, ServerID, modifyTagsCommand, []any{args}, func(err error) {
		if err != nil {
			g.api.Notifications().Error(what + ": gopls failed: " + describe(err))
		}
	})
	if err != nil {
		g.api.Notifications().Error(what + ": " + describe(err))
	}
}

// modifyTagsHere changes the tags with the editor of tags on the text of the document and applies the difference as one edit of
// the document.
func (g *golang) modifyTagsHere(ctx context.Context, what string, file goFile, mod tagMod) {
	out, err := modifyTags(file.text, file.start, file.end, mod)
	if err != nil {
		g.api.Notifications().Warn(what + ": " + describe(err))
		return
	}
	edits := textEdits(file.text, out)
	if len(edits) == 0 {
		g.api.Notifications().Info(what + ": the tags are as they should be already.")
		return
	}
	edit := extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "edit", URI: file.uri, Edits: edits}}}
	if err := g.api.Documents().Apply(ctx, edit); err != nil {
		g.api.Log().Warn("the tags cannot be applied", "uri", file.uri, "err", err)
		g.api.Notifications().Error(what + ": the edit cannot be applied: " + describe(err))
	}
}
