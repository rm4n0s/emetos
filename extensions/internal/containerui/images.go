package containerui

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// operation is something the engine is doing for the user that takes a while and shows in the Images section: a pull.
type operation struct {
	title  string
	cancel context.CancelFunc

	mu     sync.Mutex
	status string
	layers map[string][2]int64
	posted bool
}

// percent is how much of the download is done, from the layers that told their size; -1 when none did.
func (o *operation) percent() float64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	var cur, total int64
	for _, l := range o.layers {
		cur += l[0]
		total += l[1]
	}
	if total <= 0 {
		return -1
	}
	return float64(cur) / float64(total) * 100
}

// statusLine is the last thing the engine said.
func (o *operation) statusLine() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.status
}

// progress records a line of the progress of a pull, on the goroutine of the pull, and asks the UI thread to show it once for a
// burst of lines.
func (o *operation) progress(e *ext, p containerapi.PullProgress) {
	o.mu.Lock()
	if p.Status != "" {
		o.status = strings.TrimSpace(p.ID + " " + p.Status)
	}
	if p.ID != "" && p.Total > 0 {
		if o.layers == nil {
			o.layers = map[string][2]int64{}
		}
		o.layers[p.ID] = [2]int64{p.Current, p.Total}
	}
	post := !o.posted
	o.posted = true
	o.mu.Unlock()
	if post {
		e.api.Post(func() {
			o.mu.Lock()
			o.posted = false
			o.mu.Unlock()
			if v := e.views[secImages]; v != nil && v.handle != nil {
				v.handle.Refresh()
			}
		})
	}
}

// operationRows builds the rows of the operations that go on, at the top of the Images section.
func (e *ext) operationRows(bc widgets.BuildContext) []widgets.Widget {
	colors := ui.Theme(bc)
	var rows []widgets.Widget
	for _, o := range e.ops {
		o := o
		head := widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(text(bc, o.title, 13, colors.Foreground)),
			ui.NewIconButton("close", ui.IconButtonOpts{Tooltip: "Cancel", OnPressed: o.cancel}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		bar := ui.NewProgressBar(ui.ProgressBarOpts{Infinite: true})
		if p := o.percent(); p >= 0 {
			bar = ui.NewProgressBar(ui.ProgressBarOpts{Total: 100, Worked: p})
		}
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 4, 4, 4), widgets.NewColumn([]widgets.Widget{
			head, text(bc, o.statusLine(), 12, colors.DescriptionForeground), vgap(4), bar,
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})))
	}
	return rows
}

// pullImage pulls an image with its progress in the Images section; an empty reference asks for one first.
func (e *ext) pullImage(ref string) {
	if _, ok := e.ready(); !ok {
		return
	}
	if ref == "" {
		err := e.api.InputBox().Show(extension.InputBoxOptions{
			Prompt: "Image to pull (nginx:latest, ghcr.io/owner/name:tag)",
			Validate: func(v string) string {
				if strings.TrimSpace(v) == "" || strings.ContainsAny(v, " \t") {
					return "Enter an image name without spaces."
				}
				return ""
			},
		}, func(v string, ok bool) {
			if ok {
				e.startPull(strings.TrimSpace(v))
			}
		})
		if err != nil {
			e.api.Log().Warn("the input box cannot be shown", "err", err)
		}
		return
	}
	e.startPull(ref)
}

// startPull runs the pull.
func (e *ext) startPull(ref string) {
	c, ok := e.ready()
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	op := &operation{title: "Pulling " + ref, cancel: cancel, status: "Starting..."}
	e.ops = append(e.ops, op)
	if v := e.views[secImages]; v != nil {
		v.handle.Reveal(false)
	}
	e.refreshViews()
	gen := e.gen
	go func() {
		err := c.PullImage(ctx, ref, func(p containerapi.PullProgress) { op.progress(e, p) })
		cancelled := ctx.Err() != nil
		cancel()
		e.api.Post(func() {
			for i, o := range e.ops {
				if o == op {
					e.ops = append(e.ops[:i], e.ops[i+1:]...)
					break
				}
			}
			if gen == e.gen {
				e.load(resImages)
			}
			e.refreshViews()
			switch {
			case err != nil:
				e.api.Notifications().Error("Cannot pull " + ref + ": " + reasonOf(err))
			case cancelled:
				e.api.Notifications().Info("Pulling " + ref + " was cancelled.")
			default:
				e.api.Notifications().Info("Pulled " + ref + ".")
			}
		})
	}()
}

// tagImage asks for a new name for an image and gives it.
func (e *ext) tagImage(r imageRow) {
	if _, ok := e.ready(); !ok {
		return
	}
	err := e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "New name for " + r.target() + " (repository:tag)",
		Value:  r.ref,
		Validate: func(v string) string {
			if repo, _ := splitRepoTag(strings.TrimSpace(v)); repo == "" || strings.ContainsAny(v, " \t") {
				return "Enter a name such as myimage:1.0."
			}
			return ""
		},
	}, func(v string, ok bool) {
		if !ok {
			return
		}
		repo, tag := splitRepoTag(strings.TrimSpace(v))
		if tag == "" {
			tag = "latest"
		}
		e.async(func(ctx context.Context, c *containerapi.Client) []string {
			if err := c.TagImage(ctx, r.target(), repo, tag); err != nil {
				return []string{reasonOf(err)}
			}
			return nil
		}, func(f []string) {
			if len(f) > 0 {
				e.api.Notifications().Error("Cannot tag " + r.target() + ": " + f[0])
			}
		})
	})
	if err != nil {
		e.api.Log().Warn("the input box cannot be shown", "err", err)
	}
}

// inspectImage opens the JSON of an image in a read-only tab.
func (e *ext) inspectImage(r imageRow) {
	e.inspectJSON(r.label()+" (inspect)", func(ctx context.Context, c *containerapi.Client) (json.RawMessage, error) {
		info, err := c.InspectImage(ctx, r.target())
		if err != nil {
			return nil, err
		}
		return info.Raw, nil
	})
}

// imageHistory shows the layers of an image, newest first, in a read-only tab.
func (e *ext) imageHistory(r imageRow) {
	c, ok := e.ready()
	if !ok {
		return
	}
	go func() {
		layers, err := c.ImageHistory(e.ctx, r.target())
		e.api.Post(func() {
			if err != nil {
				e.api.Notifications().Error("Cannot read the history of " + r.target() + ": " + reasonOf(err))
				return
			}
			e.docs.open(r.label()+" (history)", historyText(layers, e.cfg.now()), "")
		})
	}()
}

// historyText writes the layers of an image as a table: the id, the age, the size and the command.
func historyText(layers []containerapi.HistoryEntry, now time.Time) string {
	var b strings.Builder
	b.WriteString("IMAGE         CREATED              SIZE      CREATED BY\n")
	for _, l := range layers {
		id := l.ID
		if id == "" || id == "<missing>" {
			id = "<missing>"
		} else {
			id = containerapi.ShortID(id)
		}
		b.WriteString(pad(id, 14) + pad(ago(l.Created, now), 21) + pad(humanSize(l.Size), 10) + oneLine(l.CreatedBy) + "\n")
	}
	return b.String()
}

// oneLine puts a command on one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// pad fills a string with spaces to a width.
func pad(s string, w int) string {
	if len(s) >= w {
		return s + " "
	}
	return s + strings.Repeat(" ", w-len(s))
}

// askRemoveImages asks before images are removed: how many, which, and how much they take.
func (e *ext) askRemoveImages(rows []imageRow) {
	if len(rows) == 0 {
		return
	}
	e.withUsage(func(du containerapi.DiskUsage, ok bool) {
		var lines []string
		var total int64
		inUse := false
		for _, r := range rows {
			size := r.image.Size
			if ok {
				if s, found := du.ImageSizes[trimSHA(r.image.ID)]; found {
					size = s
				}
			}
			total += size
			label := r.ref
			if label == "" {
				label = containerapi.ShortID(r.image.ID) + " (no name)"
			}
			lines = append(lines, label+"  "+humanSize(size))
			inUse = inUse || r.image.Containers > 0
		}
		lines = shortList(lines, 8)
		lines = append(lines, "Together "+humanSize(total)+" (layers other images share stay).")
		remove := func(force bool) func() { return func() { e.removeImages(rows, force) } }
		choices := []choice{{label: "Remove " + plural(len(rows), "image"), run: remove(false)}}
		if inUse {
			choices = append(choices, choice{label: "Force remove (also images in use)", run: remove(true)})
		}
		e.ask(confirmation{section: resImages, title: "Remove " + plural(len(rows), "image") + "?", lines: lines, choices: choices})
	})
}

// trimSHA removes the "sha256:" in front of an image id.
func trimSHA(id string) string { return strings.TrimPrefix(id, "sha256:") }

// removeImages removes images (or takes a name off an image that has several).
func (e *ext) removeImages(rows []imageRow, force bool) {
	e.async(func(ctx context.Context, c *containerapi.Client) []string {
		var failures []string
		for _, r := range rows {
			if _, err := c.RemoveImage(ctx, r.target(), containerapi.RemoveImageOptions{Force: force}); err != nil {
				failures = append(failures, r.target()+": "+reasonOf(err))
			}
		}
		return failures
	}, func(failures []string) {
		if len(failures) > 0 {
			e.api.Notifications().Error("Cannot remove " + strings.Join(failures, "; "))
			return
		}
		e.api.Notifications().Info("Removed " + plural(len(rows), "image") + ".")
	})
}

// withUsage reads "system df" on a goroutine and calls fn on the UI thread with it; ok is false when it cannot be read (the
// confirmation then has the counts and no sizes).
func (e *ext) withUsage(fn func(du containerapi.DiskUsage, ok bool)) {
	c, ready := e.ready()
	if !ready {
		return
	}
	gen := e.gen
	go func() {
		du, err := c.DiskUsage(e.ctx)
		e.api.Post(func() {
			if gen != e.gen {
				return
			}
			if err != nil {
				e.api.Log().Info("disk usage cannot be read", "err", err)
			}
			fn(du, err == nil)
		})
	}()
}
