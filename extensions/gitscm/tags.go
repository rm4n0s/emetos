package gitscm

import (
	"context"
	"regexp"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Tags (SPEC §10.30 "Branches, tags, merge and the states in between"): make a lightweight or an annotated tag, delete a tag here or at a
// remote, push the tags.
//
// Ported from VS Code: extensions/git/src/commands.ts (createTag, _createTag, deleteTag, deleteRemoteTag, pushTags, TagDeleteItem,
// RemoteTagDeleteItem, RemoteItem) and extensions/git/src/repository.ts (tag, deleteTag, deleteRemoteRef, pushTags)

// reTagUnsafe finds what a tag name may not have: the parts of a name that git refuses (the branch rule without "[" and "]"), which
// become a dash.
var reTagUnsafe = regexp.MustCompile(`^\.|/\.|\.\.|~|\^|:|/$|\.lock$|\.lock/|\\|\*|\s|^\s*$|\.$`)

func (x *gitscm) cmdCreateTag(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.createTag(r, nameArg(args), args) })
	return nil
}

// refArg is the commit a caller named for the tag ({"ref": "main~2"}); HEAD when none.
func refArg(args any) string {
	if m, ok := args.(map[string]any); ok {
		if s, ok := m["ref"].(string); ok {
			return s
		}
	}
	return ""
}

// createTag asks for the name of the tag and for a message; a message makes the tag annotated. Closing the box of the message still makes
// the tag, a lightweight one, as VS Code does.
func (x *gitscm) createTag(r *repository, name string, args any) {
	ref := refArg(args)
	create := func(name string, message string) {
		name = reTagUnsafe.ReplaceAllString(name, "-")
		r.mutate(mutation{
			name: "tag",
			run:  func(ctx context.Context, repo *git.Repo) error { return repo.Tag(ctx, name, message, ref) },
		})
	}
	askMessage := func(name string) {
		err := x.api.InputBox().Show(extension.InputBoxOptions{
			Prompt: "Message", Message: "Please provide a message to annotate the tag", IgnoreFocusOut: true,
		}, func(message string, ok bool) {
			if !ok {
				message = ""
			}
			create(name, message)
		})
		if err != nil {
			x.logUnavailable("cannot ask for the message of the tag", err)
		}
	}
	if name != "" {
		askMessage(name)
		return
	}
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Tag name", Message: "Please provide a tag name", IgnoreFocusOut: true,
	}, func(value string, ok bool) {
		if ok && value != "" {
			askMessage(value)
		}
	})
	if err != nil {
		x.logUnavailable("cannot ask for the name of the tag", err)
	}
}

func (x *gitscm) cmdDeleteTag(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if name := nameArg(args); name != "" {
			x.runDeleteTag(r, name)
			return
		}
		x.loadRefs(r, x.refQuery("refs/tags"), func(refs []git.Ref) {
			entries := x.refEntries(r, refs, refListOptions{types: []git.RefType{git.RefTag}})
			if !hasChoice(entries) {
				entries = []refEntry{{item: extension.QuickPickItem{Label: "$(info) This repository has no tags."}}}
			}
			err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a tag to delete", Items: pickItems(entries)}, func(i int) {
				if i >= 0 && i < len(entries) && entries[i].ref != nil && !r.disposed {
					x.runDeleteTag(r, entries[i].ref.Name)
				}
			})
			if err != nil {
				x.logUnavailable("cannot show the tags", err)
			}
		})
	})
	return nil
}

func (x *gitscm) runDeleteTag(r *repository, name string) {
	r.mutate(mutation{name: "delete tag", run: func(ctx context.Context, repo *git.Repo) error { return repo.DeleteTag(ctx, name) }})
}

// pushRemotes are the remotes of a repository that can be pushed to, read from its configuration now: a remote added since the
// repository was opened counts.
func (r *repository) pushRemotes() []git.Remote {
	r.readRemotes()
	var out []git.Remote
	for _, rem := range r.remotes {
		if rem.PushURL != "" && !rem.ReadOnly {
			out = append(out, rem)
		}
	}
	return out
}

func (x *gitscm) cmdDeleteRemoteTag(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		remotes := r.pushRemotes()
		switch len(remotes) {
		case 0:
			x.api.Notifications().Error("Your repository has no remotes configured to push to.")
		case 1:
			x.pickRemoteTag(r, remotes[0].Name, nameArg(args))
		default:
			items := make([]extension.QuickPickItem, len(remotes))
			for i, rem := range remotes {
				items[i] = extension.QuickPickItem{Label: "$(cloud) " + rem.Name, Description: rem.FetchURL}
			}
			err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a remote to delete a tag from", Items: items}, func(i int) {
				if i >= 0 && i < len(remotes) && !r.disposed {
					x.pickRemoteTag(r, remotes[i].Name, nameArg(args))
				}
			})
			if err != nil {
				x.logUnavailable("cannot show the remotes", err)
			}
		}
	})
	return nil
}

// pickRemoteTag lists the tags a remote has and deletes the one picked there. A caller that named the tag is not asked.
func (x *gitscm) pickRemoteTag(r *repository, remote, name string) {
	del := func(tag string) {
		r.mutate(mutation{
			name: "delete remote tag",
			run:  func(ctx context.Context, repo *git.Repo) error { return repo.DeleteRemoteRef(ctx, remote, tag, false) },
		})
	}
	if name != "" {
		del(name)
		return
	}
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		tags, err := repo.RemoteTags(ctx, remote)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("ls-remote", r, err, nil)
				return
			}
			items := make([]extension.QuickPickItem, 0, len(tags))
			for _, t := range tags {
				items = append(items, extension.QuickPickItem{Label: "$(tag) " + t.Name, Description: "Remote tag at " + x.shortHash(t.Commit)})
			}
			if len(items) == 0 {
				items = append(items, extension.QuickPickItem{Label: `$(info) Remote "` + remote + `" has no tags.`})
			}
			err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a remote tag to delete", Items: items}, func(i int) {
				if i >= 0 && i < len(tags) && !r.disposed {
					del(tags[i].Name)
				}
			})
			if err != nil {
				x.logUnavailable("cannot show the tags of the remote", err)
			}
		})
	}()
}
