package testing

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"
)

// ControllerOptions are what a controller is registered with.
type ControllerOptions struct {
	// ID is the id of the controller: not empty and without the IDSeparator; Label is its name.
	ID, Label string
	// Resolve loads the children of the item with the id, or of the whole tree for the controller's own id (VS Code's resolveHandler);
	// nil when the controller does not load on demand.
	Resolve func(ctx context.Context, id string) error
	// Refresh is called by the Refresh Tests command; nil when the controller cannot be refreshed.
	Refresh func(ctx context.Context) error
}

// controller is the tree and the profiles of one provider of tests.
type controller struct {
	id, label string
	resolve   func(ctx context.Context, id string) error
	refresh   func(ctx context.Context) error
	items     map[string]Item
	children  map[string][]string // by parent id (the controller's id for the top items), in the order they were added
	profiles  []*profile
	profSeq   int
	disposed  bool
}

// Controller is a controller registered with a Service.
type Controller struct {
	svc *Service
	id  string
}

// RegisterController adds a controller with an empty tree. It fails with TestingControllerInvalid for an id that is empty or holds the
// IDSeparator and for no label, and with TestingControllerDuplicate when the id is in use.
func (s *Service) RegisterController(o ControllerOptions) (*Controller, error) {
	if o.ID == "" || strings.Contains(o.ID, IDSeparator) || strings.TrimSpace(o.Label) == "" {
		return nil, errors.New("TestingControllerInvalid", "a controller needs an id without NUL bytes and a label", "id", o.ID)
	}
	s.mu.Lock()
	if _, ok := s.controllers[o.ID]; ok {
		s.mu.Unlock()
		return nil, errors.New("TestingControllerDuplicate", "a controller has this id already", "id", o.ID)
	}
	s.controllers[o.ID] = &controller{
		id: o.ID, label: o.Label, resolve: o.Resolve, refresh: o.Refresh,
		items: map[string]Item{}, children: map[string][]string{},
	}
	s.ctrlOrder = append(s.ctrlOrder, o.ID)
	s.emitLocked(Event{Kind: EventControllers, ControllerID: o.ID})
	s.unlock()
	return &Controller{svc: s, id: o.ID}, nil
}

// ControllerInfo describes a registered controller.
type ControllerInfo struct{ ID, Label string }

// Controllers lists the controllers, in the order they were registered.
func (s *Service) Controllers() []ControllerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ControllerInfo, 0, len(s.ctrlOrder))
	for _, id := range s.ctrlOrder {
		out = append(out, ControllerInfo{ID: id, Label: s.controllers[id].label})
	}
	return out
}

// ID is the id of the controller.
func (c *Controller) ID() string { return c.id }

// SetLabel renames the controller.
func (c *Controller) SetLabel(label string) {
	s := c.svc
	s.mu.Lock()
	if ctl := s.controllers[c.id]; ctl != nil && strings.TrimSpace(label) != "" && ctl.label != label {
		ctl.label = label
		s.emitLocked(Event{Kind: EventControllers, ControllerID: c.id})
	}
	s.unlock()
}

// Upsert adds the item to the tree, or replaces the item that has its id, keeping its place among its siblings and its children.
// ControllerID and ParentID of the item are set by the model from its ID, which must be a JoinID under this controller's id. It fails
// with TestingItemInvalid for an id that is not under the controller or is the controller's own, and with TestingItemParentUnknown when
// the parent is not in the tree.
func (c *Controller) Upsert(item Item) error {
	s := c.svc
	s.mu.Lock()
	ctl := s.controllers[c.id]
	if ctl == nil || ctl.disposed {
		s.mu.Unlock()
		return errors.New("TestingControllerUnknown", "the controller was removed", "id", c.id)
	}
	if !IsAncestorID(c.id, item.ID) || strings.HasSuffix(item.ID, IDSeparator) {
		s.mu.Unlock()
		return errors.New("TestingItemInvalid", "the id of an item is the controller's id, a separator and the ids down the tree", "id", item.ID, "controller", c.id)
	}
	parent := ParentID(item.ID)
	if _, ok := ctl.items[parent]; parent != c.id && !ok {
		s.mu.Unlock()
		return errors.New("TestingItemParentUnknown", "the parent of the item is not in the tree", "id", item.ID, "controller", c.id)
	}
	item.ControllerID, item.ParentID = c.id, parent
	item.Tags = slices.Clone(item.Tags)
	if item.Range != nil {
		r := *item.Range
		item.Range = &r
	}
	if _, exists := ctl.items[item.ID]; !exists {
		ctl.children[parent] = append(ctl.children[parent], item.ID)
	}
	ctl.items[item.ID] = item
	s.emitLocked(Event{Kind: EventTree, ControllerID: c.id, IDs: []string{item.ID}})
	s.unlock()
	return nil
}

// Remove takes the item and its descendants out of the tree; the results keep what they know of them.
func (c *Controller) Remove(id string) {
	s := c.svc
	s.mu.Lock()
	ctl := s.controllers[c.id]
	if ctl == nil {
		s.mu.Unlock()
		return
	}
	if _, ok := ctl.items[id]; ok {
		parent := ctl.items[id].ParentID
		ctl.children[parent] = slices.DeleteFunc(ctl.children[parent], func(x string) bool { return x == id })
		ids := ctl.dropLocked(id)
		s.emitLocked(Event{Kind: EventTree, ControllerID: c.id, IDs: ids, Removed: true})
	}
	s.unlock()
}

// dropLocked deletes the item and its descendants and lists their ids.
func (ctl *controller) dropLocked(id string) []string {
	ids := []string{id}
	for _, child := range ctl.children[id] {
		ids = append(ids, ctl.dropLocked(child)...)
	}
	delete(ctl.items, id)
	delete(ctl.children, id)
	return ids
}

// Dispose removes the controller with its items and profiles, and stops the continuous runs of its profiles.
func (c *Controller) Dispose() {
	s := c.svc
	s.mu.Lock()
	ctl := s.controllers[c.id]
	if ctl == nil {
		s.mu.Unlock()
		return
	}
	ctl.disposed = true
	delete(s.controllers, c.id)
	s.ctrlOrder = slices.DeleteFunc(s.ctrlOrder, func(x string) bool { return x == c.id })
	s.stopControllerContinuousLocked(c.id)
	s.emitLocked(Event{Kind: EventTree, ControllerID: c.id, IDs: []string{c.id}, Removed: true})
	s.emitLocked(Event{Kind: EventControllers, ControllerID: c.id})
	s.emitLocked(Event{Kind: EventProfiles, ControllerID: c.id})
	s.unlock()
}

// Item returns the item with the id.
func (s *Service) Item(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.itemLocked(id)
}

func (s *Service) itemLocked(id string) (Item, bool) {
	ctl := s.controllers[ControllerOf(id)]
	if ctl == nil {
		return Item{}, false
	}
	it, ok := ctl.items[id]
	return it, ok
}

// Children lists the children of the item, or the top items of a controller for its id, in the order they were added.
func (s *Service) Children(id string) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.childrenLocked(id)
}

func (s *Service) childrenLocked(id string) []Item {
	ctl := s.controllers[ControllerOf(id)]
	if ctl == nil {
		return nil
	}
	out := make([]Item, 0, len(ctl.children[id]))
	for _, c := range ctl.children[id] {
		out = append(out, ctl.items[c])
	}
	return out
}

// Walk calls fn for the item with the id and for each of its descendants, parents first; for a controller's id it starts with its top
// items. fn returning false skips the descendants of that item. The tree may not be changed from fn.
func (s *Service) Walk(id string, fn func(Item) bool) {
	s.mu.Lock()
	var items []Item
	var rec func(id string)
	rec = func(id string) {
		for _, c := range s.childrenLocked(id) {
			items = append(items, c)
			rec(c.ID)
		}
	}
	if it, ok := s.itemLocked(id); ok {
		items = append(items, it)
		rec(id)
	} else if s.controllers[id] != nil {
		rec(id)
	}
	s.mu.Unlock()
	skip := ""
	for _, it := range items {
		if skip != "" && IsAncestorID(skip, it.ID) {
			continue
		}
		skip = ""
		if !fn(it) {
			skip = it.ID
		}
	}
}

// ItemCount is the number of items in the tree of a controller.
func (s *Service) ItemCount(controllerID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctl := s.controllers[controllerID]; ctl != nil {
		return len(ctl.items)
	}
	return 0
}

// ResolveItem asks the controller to load the children of the item, or of the whole tree for a controller's id (VS Code's
// resolveChildren). It does nothing for a controller with no resolve function, and fails with TestingControllerUnknown for an id of no
// controller. The function is called on the goroutine of the caller, with no lock held.
func (s *Service) ResolveItem(ctx context.Context, id string) error {
	s.mu.Lock()
	ctl := s.controllers[ControllerOf(id)]
	var fn func(context.Context, string) error
	if ctl != nil {
		fn = ctl.resolve
	}
	s.mu.Unlock()
	if ctl == nil {
		return errors.New("TestingControllerUnknown", "no controller has this id", "id", ControllerOf(id))
	}
	if fn == nil {
		return nil
	}
	return fn(ctx, id)
}

// RefreshTests asks the controller to read its tests again, or every controller that can be refreshed for an empty id (VS Code's
// refreshTests). The functions are called on the goroutine of the caller, one after the other, with no lock held; the first error ends
// the refresh. It fails with TestingControllerUnknown for an id of no controller.
func (s *Service) RefreshTests(ctx context.Context, controllerID string) error {
	s.mu.Lock()
	var fns []func(context.Context) error
	known := controllerID == ""
	for _, id := range s.ctrlOrder {
		if ctl := s.controllers[id]; (controllerID == "" || id == controllerID) && ctl != nil {
			known = true
			if ctl.refresh != nil {
				fns = append(fns, ctl.refresh)
			}
		}
	}
	s.mu.Unlock()
	if !known {
		return errors.New("TestingControllerUnknown", "no controller has this id", "id", controllerID)
	}
	for _, fn := range fns {
		if err := fn(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ---- profiles

// ProfileSpec describes a run profile.
type ProfileSpec struct {
	// Label is the name of the profile and Group what it does.
	Label string
	Group Group
	// IsDefault makes the profile one of the defaults of its group, unless the user chose otherwise before (Service.SetGroupDefaults).
	IsDefault bool
	// Tag limits the profile to the items with the tag.
	Tag string
	// SupportsContinuous says that Run honors ProfileRequest.Continuous.
	SupportsContinuous bool
	// Run runs the tests of the request; see ProfileRequest. It is called with no lock held.
	Run func(ctx context.Context, req ProfileRequest) error
	// Configure is called when the user asks to configure the profile; nil when there is nothing to configure.
	Configure func()
	// Trigger says whether the change of the file with the URI starts the continuous run of the item again; nil is the item's own URI.
	Trigger func(item Item, uri string) bool
	// LoadCoverage loads the details of a file's coverage; nil when the profile has none.
	LoadCoverage CoverageLoader
}

// profile is a registered run profile.
type profile struct {
	spec       ProfileSpec
	controller string
	id         int
	isDefault  bool
	initial    bool
}

// profileKey identifies a profile across sessions, to keep the choice of its default.
type profileKey struct {
	controller, label string
	group             Group
}

func (p *profile) key() profileKey { return profileKey{p.controller, p.spec.Label, p.spec.Group} }

// Profile is the public view of a run profile.
type Profile struct {
	// ControllerID and ID identify the profile; the id counts from 1 in each controller.
	ControllerID string
	ID           int
	Label        string
	Group        Group
	// IsDefault says that the group runs the profile without asking, Tag limits it to the items with the tag.
	IsDefault          bool
	Tag                string
	SupportsContinuous bool
	// HasConfigure says that the profile can be configured.
	HasConfigure bool
}

func (p *profile) info() Profile {
	return Profile{
		ControllerID: p.controller, ID: p.id, Label: p.spec.Label, Group: p.spec.Group, IsDefault: p.isDefault, Tag: p.spec.Tag,
		SupportsContinuous: p.spec.SupportsContinuous, HasConfigure: p.spec.Configure != nil,
	}
}

// ProfileRef names a profile.
type ProfileRef struct {
	ControllerID string
	ID           int
}

// Ref is the reference of the profile.
func (p Profile) Ref() ProfileRef { return ProfileRef{p.ControllerID, p.ID} }

// sortProfiles puts the defaults first and sorts by label (VS Code's sorter).
func sortProfiles(ps []*profile) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].isDefault != ps[j].isDefault {
			return ps[i].isDefault
		}
		return strings.ToLower(ps[i].spec.Label) < strings.ToLower(ps[j].spec.Label)
	})
}

// AddProfile adds a run profile and returns its id. It fails with TestingProfileInvalid for no label, an unknown group or no Run
// function.
func (c *Controller) AddProfile(spec ProfileSpec) (int, error) {
	s := c.svc
	if strings.TrimSpace(spec.Label) == "" || !spec.Group.Valid() || spec.Run == nil {
		return 0, errors.New("TestingProfileInvalid", "a run profile needs a label, a group and a function that runs", "controller", c.id, "label", spec.Label)
	}
	s.mu.Lock()
	ctl := s.controllers[c.id]
	if ctl == nil {
		s.mu.Unlock()
		return 0, errors.New("TestingControllerUnknown", "the controller was removed", "id", c.id)
	}
	ctl.profSeq++
	p := &profile{spec: spec, controller: c.id, id: ctl.profSeq, isDefault: spec.IsDefault, initial: spec.IsDefault}
	if v, ok := s.userDefault[p.key()]; ok {
		p.isDefault = v
	}
	ctl.profiles = append(ctl.profiles, p)
	sortProfiles(ctl.profiles)
	s.emitLocked(Event{Kind: EventProfiles, ControllerID: c.id})
	s.unlock()
	return p.id, nil
}

// UpdateProfile changes the label or the default of a profile; nil leaves the member as it is. A change of the default is remembered as
// the user's choice (VS Code does the same for an extension that changes it).
func (c *Controller) UpdateProfile(id int, label *string, isDefault *bool) {
	s := c.svc
	s.mu.Lock()
	if p := s.profileLocked(ProfileRef{c.id, id}); p != nil {
		if label != nil && strings.TrimSpace(*label) != "" {
			p.spec.Label = *label
		}
		if isDefault != nil {
			s.setDefaultLocked(p, *isDefault)
		}
		sortProfiles(s.controllers[c.id].profiles)
		s.emitLocked(Event{Kind: EventProfiles, ControllerID: c.id})
	}
	s.unlock()
}

// RemoveProfile removes a profile, and stops the continuous runs that use it.
func (c *Controller) RemoveProfile(id int) {
	s := c.svc
	s.mu.Lock()
	if ctl := s.controllers[c.id]; ctl != nil {
		before := len(ctl.profiles)
		ctl.profiles = slices.DeleteFunc(ctl.profiles, func(p *profile) bool { return p.id == id })
		if len(ctl.profiles) != before {
			s.stopProfileContinuousLocked(ProfileRef{c.id, id})
			s.emitLocked(Event{Kind: EventProfiles, ControllerID: c.id})
		}
	}
	s.unlock()
}

func (s *Service) profileLocked(ref ProfileRef) *profile {
	ctl := s.controllers[ref.ControllerID]
	if ctl == nil {
		return nil
	}
	for _, p := range ctl.profiles {
		if p.id == ref.ID {
			return p
		}
	}
	return nil
}

// setDefaultLocked sets the default of a profile and notes it as the user's choice when it is not the one the profile was made with.
func (s *Service) setDefaultLocked(p *profile, isDefault bool) {
	p.isDefault = isDefault
	if isDefault != p.initial {
		s.userDefault[p.key()] = isDefault
	} else {
		delete(s.userDefault, p.key())
	}
}

// Profiles lists the profiles of a controller, the defaults first and then by label; for an empty id, those of all controllers.
func (s *Service) Profiles(controllerID string) []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Profile
	for _, id := range s.ctrlOrder {
		if controllerID != "" && id != controllerID {
			continue
		}
		for _, p := range s.controllers[id].profiles {
			out = append(out, p.info())
		}
	}
	return out
}

// GroupDefaults are the profiles the group runs without asking (VS Code's getGroupDefaultProfiles): those of the group that are
// defaults, and when there are none the first profile of the group. With a controller id only its profiles are looked at.
func (s *Service) GroupDefaults(group Group, controllerID string) []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groupDefaultsLocked(group, controllerID)
}

func (s *Service) groupDefaultsLocked(group Group, controllerID string) []Profile {
	var all []*profile
	for _, id := range s.ctrlOrder {
		if controllerID == "" || id == controllerID {
			all = append(all, s.controllers[id].profiles...)
		}
	}
	var out []Profile
	for _, p := range all {
		if p.spec.Group == group && p.isDefault {
			out = append(out, p.info())
		}
	}
	if len(out) == 0 {
		for _, p := range all {
			if p.spec.Group == group {
				return []Profile{p.info()}
			}
		}
	}
	return out
}

// SetGroupDefaults makes the profiles the defaults of the group and the others of the group not (VS Code's setGroupDefaultProfiles); a
// profile of another group that has the label of one that changed follows it. The choice is saved with the state.
func (s *Service) SetGroupDefaults(group Group, refs []ProfileRef) {
	s.mu.Lock()
	for _, id := range s.ctrlOrder {
		ctl := s.controllers[id]
		for _, p := range ctl.profiles {
			if p.spec.Group == group {
				s.setDefaultLocked(p, slices.Contains(refs, ProfileRef{id, p.id}))
			}
		}
		for _, p := range ctl.profiles {
			if p.spec.Group == group {
				continue
			}
			for _, m := range ctl.profiles {
				if m.spec.Group == group && m.spec.Label == p.spec.Label {
					s.setDefaultLocked(p, m.isDefault)
					break
				}
			}
		}
		sortProfiles(ctl.profiles)
	}
	s.emitLocked(Event{Kind: EventProfiles})
	s.unlock()
}

// ConfigureProfile calls the configure function of the profile, with no lock held. It fails with TestingProfileUnknown.
func (s *Service) ConfigureProfile(ref ProfileRef) error {
	s.mu.Lock()
	p := s.profileLocked(ref)
	var fn func()
	if p != nil {
		fn = p.spec.Configure
	}
	s.mu.Unlock()
	if p == nil {
		return errors.New("TestingProfileUnknown", "no profile has this id", "controller", ref.ControllerID, "profile", ref.ID)
	}
	if fn != nil {
		fn()
	}
	return nil
}

// Capabilities are the groups that some profile of a controller can run for an item, as the context keys testing.hasRunnableTests,
// hasDebuggableTests and hasCoverableTests, and whether some profile is a second one of its group or can be configured or run
// continuously.
type Capabilities struct {
	Run, Debug, Coverage                       bool
	HasNonDefault, HasConfigurable, Continuous bool
}

// Capabilities reports what the profiles of the controller can do with the item; an empty id is every profile of every controller.
func (s *Service) Capabilities(id string) Capabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	var item Item
	filter := false
	if id != "" {
		if it, ok := s.itemLocked(id); ok {
			item, filter = it, true
		}
	}
	var c Capabilities
	seen := map[Group]bool{}
	for _, cid := range s.ctrlOrder {
		if id != "" && cid != ControllerOf(id) {
			continue
		}
		for _, p := range s.controllers[cid].profiles {
			if filter && p.spec.Tag != "" && !item.HasTag(p.spec.Tag) {
				continue
			}
			if seen[p.spec.Group] {
				c.HasNonDefault = true
			}
			seen[p.spec.Group] = true
			switch p.spec.Group {
			case GroupRun:
				c.Run = true
			case GroupDebug:
				c.Debug = true
			case GroupCoverage:
				c.Coverage = true
			}
			c.HasConfigurable = c.HasConfigurable || p.spec.Configure != nil
			c.Continuous = c.Continuous || p.spec.SupportsContinuous
		}
	}
	return c
}
