package extension

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
)

// The Testing API (SPEC §10.32 "The contract", step 26.1; after the tests namespace of vscode.d.ts and extHostTesting.ts). A language
// extension makes a TestController (Tests().CreateTestController) and fills its Items with the tests it finds; it makes run profiles
// (Run, Debug, Coverage) whose handler runs the tests of a TestRunRequest, and reports what happens in a TestRun (CreateTestRun):
// tests enqueued, started, passed, failed, errored or skipped with their messages, the output of the run and the coverage it measured.
//
// Where a comment does not say otherwise a method may be called from any goroutine. The functions the extension gives (the handlers of a
// controller and of its profiles) are called by the host on a goroutine of its own, never on the UI thread, with a context that ends when
// the work is no longer wanted; they must not touch the widgets (use Post). A panic in one deactivates the extension
// (ExtensionTestingPanicked).

// TestRunKind says what a run profile does with the tests (VS Code's TestRunProfileKind).
type TestRunKind int

const (
	// TestKindRun runs the tests.
	TestKindRun TestRunKind = iota + 1
	// TestKindDebug runs the tests under a debugger.
	TestKindDebug
	// TestKindCoverage runs the tests and measures the coverage.
	TestKindCoverage
)

// TestNoDuration is the duration of a result that has none (VS Code's undefined).
const TestNoDuration time.Duration = -1

// TestItemData is what describes a test item (VS Code's TestItem properties).
type TestItemData struct {
	// URI is the file the item is in (file:///path); empty when it has none.
	URI string
	// Label is the text of the item, Description more text after it, and SortText what the items are sorted by when the explorer sorts
	// by name (the label when empty).
	Label, Description, SortText string
	// Range is the place of the item in its file; nil when unknown.
	Range *Range
	// Tags are the tags of the item; a run profile with a tag runs only the items that carry it.
	Tags []string
	// Error is shown as the item's error row (VS Code's TestItem.error) when the extension could not read the item.
	Error string
	// Busy says that the extension is working on the item (resolving it).
	Busy bool
	// CanResolveChildren says that the item's children are loaded when it is expanded (the controller's ResolveHandler).
	CanResolveChildren bool
}

// TestItem is a test, or a group of tests (a folder, a file, a suite), in a tree (VS Code's TestItem). Its children are in Children().
// Making one does not put it anywhere: add it to the Items of a controller or to the children of another item. All methods are safe for
// concurrent use; an item belongs to one tree for its life, and is added to a tree once at a time.
type TestItem struct {
	id       string
	tree     atomic.Pointer[testTree]
	holder   atomic.Pointer[TestItemCollection]
	data     TestItemData
	children *TestItemCollection
}

// NewTestItem makes an item that is in no tree. The id is unique among the siblings; it must not hold a NUL byte.
func NewTestItem(id, label, uri string) *TestItem {
	it := &TestItem{id: id, data: TestItemData{Label: label, URI: uri}}
	it.tree.Store(&testTree{})
	it.children = &TestItemCollection{owner: it}
	return it
}

// ID is the id of the item among its siblings.
func (it *TestItem) ID() string { return it.id }

// Children are the children of the item.
func (it *TestItem) Children() *TestItemCollection { return it.children }

// Parent is the item that has this one as a child, or nil for the item of a controller's root or one that is in no tree.
func (it *TestItem) Parent() *TestItem {
	if h := it.holder.Load(); h != nil {
		return h.owner
	}
	return nil
}

// Data is a copy of the item's properties.
func (it *TestItem) Data() TestItemData {
	t := it.lockTree()
	defer t.mu.Unlock()
	return cloneItemData(it.data)
}

// Update changes the item's properties with fn, which receives a copy, and reports the change to the tree the item is in.
func (it *TestItem) Update(fn func(d *TestItemData)) {
	t := it.lockTree()
	defer t.mu.Unlock()
	d := cloneItemData(it.data)
	fn(&d)
	it.data = d
	if t.observer != nil && it.holder.Load() != nil {
		t.observer(TestTreeChange{Kind: TestItemChanged, Item: it, Parent: it.Parent(), Path: it.pathLocked(), Data: cloneItemData(d)})
	}
}

// lockTree locks the tree the item is in, which may change while it waits for the lock.
func (it *TestItem) lockTree() *testTree {
	for {
		t := it.tree.Load()
		t.mu.Lock()
		if it.tree.Load() == t {
			return t
		}
		t.mu.Unlock()
	}
}

// pathLocked is the ids from the root of the tree down to the item. The tree is locked.
func (it *TestItem) pathLocked() []string {
	var path []string
	for x := it; x != nil; x = x.Parent() {
		path = append(path, x.id)
	}
	slices.Reverse(path)
	return path
}

func cloneItemData(d TestItemData) TestItemData {
	d.Tags = slices.Clone(d.Tags)
	if d.Range != nil {
		r := *d.Range
		d.Range = &r
	}
	return d
}

// TestTreeChangeKind says what happened to an item of a tree that has an observer.
type TestTreeChangeKind int

const (
	// TestItemAdded is an item added to the tree; its descendants are reported after it, each as added.
	TestItemAdded TestTreeChangeKind = iota + 1
	// TestItemRemoved is an item taken out of the tree with its descendants.
	TestItemRemoved
	// TestItemChanged is a change of the properties of an item of the tree.
	TestItemChanged
)

// TestTreeChange is a change of a tree made with NewTestTree.
type TestTreeChange struct {
	Kind TestTreeChangeKind
	// Item is the item, Parent the item it is a child of (nil at the root) and Path the ids from the root down to the item.
	Item   *TestItem
	Parent *TestItem
	Path   []string
	// Data are the properties of the item when it was added or changed.
	Data TestItemData
}

// testTree is what the items of one tree share: the lock, and the observer of the changes.
type testTree struct {
	mu       sync.Mutex
	observer func(TestTreeChange)
}

// TestItemCollection is the children of an item, or the items of a controller (VS Code's TestItemCollection). Its methods are safe for
// concurrent use.
type TestItemCollection struct {
	owner *TestItem
	tree  *testTree // set for the root of a tree made with NewTestTree
	items []*TestItem
}

// NewTestTree makes the root collection of a tree whose changes observer receives: the items added to it, and to the children of its
// items, are reported while the tree is locked, in the order of the changes, so the observer must not call the methods of the tree. A
// host or a fake makes one for each controller.
func NewTestTree(observer func(TestTreeChange)) *TestItemCollection {
	return &TestItemCollection{tree: &testTree{observer: observer}}
}

// lock locks the tree the collection is in and returns it.
func (c *TestItemCollection) lock() *testTree {
	if c.owner == nil {
		c.tree.mu.Lock()
		return c.tree
	}
	return c.owner.lockTree()
}

// Size is the number of items.
func (c *TestItemCollection) Size() int {
	t := c.lock()
	defer t.mu.Unlock()
	return len(c.items)
}

// All lists the items, in the order they were added.
func (c *TestItemCollection) All() []*TestItem {
	t := c.lock()
	defer t.mu.Unlock()
	return slices.Clone(c.items)
}

// Get returns the item with the id, or nil.
func (c *TestItemCollection) Get(id string) *TestItem {
	t := c.lock()
	defer t.mu.Unlock()
	return c.getLocked(id)
}

func (c *TestItemCollection) getLocked(id string) *TestItem {
	for _, it := range c.items {
		if it.id == id {
			return it
		}
	}
	return nil
}

// Add puts the item in the collection; an item with the same id that was there is replaced (with its descendants). It fails with
// ExtensionTestItemInvalid for no item, an empty id or an id with a NUL byte, and with ExtensionTestItemOwned for an item that is in
// another collection already, or is the owner of this one or an ancestor of it.
func (c *TestItemCollection) Add(item *TestItem) error {
	t := c.lock()
	defer t.mu.Unlock()
	return c.addLocked(t, item)
}

func (c *TestItemCollection) addLocked(t *testTree, item *TestItem) error {
	switch {
	case item == nil || item.id == "" || strings.ContainsRune(item.id, 0):
		return errors.New("ExtensionTestItemInvalid", "a test item needs an id that is not empty and has no NUL byte")
	case item.holder.Load() == c:
		return nil
	case item.holder.Load() != nil:
		return errors.New("ExtensionTestItemOwned", "the test item is in a collection already; delete it there first", "id", item.id)
	}
	for x := c.owner; x != nil; x = x.Parent() {
		if x == item {
			return errors.New("ExtensionTestItemOwned", "a test item cannot be added to its own descendants", "id", item.id)
		}
	}
	if old := c.getLocked(item.id); old != nil {
		if old == item {
			return nil
		}
		c.removeLocked(t, old)
	}
	c.items = append(c.items, item)
	item.holder.Store(c)
	c.attach(t, item)
	return nil
}

// attach moves the item and its descendants into the tree and reports them, the item first.
func (c *TestItemCollection) attach(t *testTree, item *TestItem) {
	item.tree.Store(t)
	if t.observer != nil {
		t.observer(TestTreeChange{Kind: TestItemAdded, Item: item, Parent: c.owner, Path: item.pathLocked(), Data: cloneItemData(item.data)})
	}
	for _, child := range item.children.items {
		item.children.attach(t, child)
	}
}

// Delete takes the item with the id out, with its descendants, and reports whether there was one. The item is in no tree afterwards and
// may be added again.
func (c *TestItemCollection) Delete(id string) bool {
	t := c.lock()
	defer t.mu.Unlock()
	old := c.getLocked(id)
	if old == nil {
		return false
	}
	c.removeLocked(t, old)
	return true
}

func (c *TestItemCollection) removeLocked(t *testTree, item *TestItem) {
	if t.observer != nil {
		t.observer(TestTreeChange{Kind: TestItemRemoved, Item: item, Parent: c.owner, Path: item.pathLocked(), Data: cloneItemData(item.data)})
	}
	c.items = slices.DeleteFunc(c.items, func(x *TestItem) bool { return x == item })
	item.holder.Store(nil)
	detach(item)
}

// detach gives the item and its descendants a tree of their own.
func detach(item *TestItem) {
	item.tree.Store(&testTree{})
	for _, child := range item.children.items {
		detach(child)
	}
}

// Replace makes the collection hold exactly the items: the ones with an id that is not among them are deleted, the new ones are added at
// the end, and an item with the id of one that is there is put in its place when it is another item. It validates all the items before
// it changes anything and fails like Add.
func (c *TestItemCollection) Replace(items []*TestItem) error {
	t := c.lock()
	defer t.mu.Unlock()
	seen := map[string]bool{}
	for _, it := range items {
		switch {
		case it == nil || it.id == "" || strings.ContainsRune(it.id, 0):
			return errors.New("ExtensionTestItemInvalid", "a test item needs an id that is not empty and has no NUL byte")
		case seen[it.id]:
			return errors.New("ExtensionTestItemInvalid", "two of the test items have the same id", "id", it.id)
		case it.holder.Load() != nil && it.holder.Load() != c:
			return errors.New("ExtensionTestItemOwned", "the test item is in a collection already; delete it there first", "id", it.id)
		}
		seen[it.id] = true
	}
	for _, old := range slices.Clone(c.items) {
		if !seen[old.id] {
			c.removeLocked(t, old)
		}
	}
	for _, it := range items {
		if err := c.addLocked(t, it); err != nil {
			return err
		}
	}
	return nil
}

// TestMessage is something to tell about a test that failed or errored (VS Code's TestMessage).
type TestMessage struct {
	// Message is the text. With Markdown it is Markdown, otherwise plain text.
	Message  string
	Markdown bool
	// ExpectedOutput and ActualOutput are shown as a diff when both are not empty.
	ExpectedOutput, ActualOutput string
	// Location is where the message belongs, usually the line of the failed assertion; nil shows it at the test.
	Location *Location
	// StackTrace are the frames of the failure, the innermost first.
	StackTrace []TestStackFrame
	// ContextValue is a value for the menus of the message.
	ContextValue string
}

// TestStackFrame is a line of the stack trace of a failure (VS Code's TestMessageStackFrame).
type TestStackFrame struct {
	// Label is the text (the function), URI the file and Position the place in it; both may be empty.
	Label    string
	URI      string
	Position *Position
}

// TestRunRequest says what a run is asked to run (VS Code's TestRunRequest). The handler of a profile receives the request and gives it
// to CreateTestRun, which joins the run to the request.
type TestRunRequest struct {
	// Include are the items to run; nil runs all the tests of the controller.
	Include []*TestItem
	// Exclude are the items not to run; they are children of the included ones.
	Exclude []*TestItem
	// Profile is the profile that runs them; nil for a run an extension starts on its own.
	Profile TestRunProfile
	// Continuous says that the tests run again when the files they depend on change (the profile must support it); the handler returns
	// when it has started the run, and the context of the handler ends when the continuous run is stopped.
	Continuous bool
	// PreserveFocus says that the run was not asked for with the Testing view in front.
	PreserveFocus bool
}

// TestRunHandler runs the tests of a request with the profile it is the handler of (VS Code's runHandler). It may return when the
// tests are run, or earlier: the run goes on until every TestRun it made ended. ctx ends when the user cancels the run.
type TestRunHandler func(ctx context.Context, request *TestRunRequest) error

// TestRunProfileOptions are the options of CreateRunProfile.
type TestRunProfileOptions struct {
	// IsDefault makes the profile one of those the group runs without being asked which.
	IsDefault bool
	// Tag limits the profile to the items that carry the tag.
	Tag string
	// SupportsContinuousRun says that the handler honors TestRunRequest.Continuous.
	SupportsContinuousRun bool
	// ConfigureHandler is called when the user asks to configure the profile (the gear of the profile picker); nil when it has nothing
	// to configure.
	ConfigureHandler func()
	// LoadDetailedCoverage gives the lines, branches and declarations of a file the run reported coverage for (VS Code's
	// loadDetailedCoverage). It is called when the file is opened with coverage on, with a context that ends when the user moves on.
	LoadDetailedCoverage func(ctx context.Context, file FileCoverage) (CoverageDetails, error)
	// ContinuousTrigger says whether the change of a file starts the continuous run of the item again; nil starts it again when the
	// file is the item's URI. uri is file:///path of a changed file.
	ContinuousTrigger func(item *TestItem, uri string) bool
}

// TestRunProfile is a way to run tests: a label, a kind and a handler (VS Code's TestRunProfile).
type TestRunProfile interface {
	// Label is the name of the profile and Kind says what it does.
	Label() string
	Kind() TestRunKind
	// IsDefault reports whether the group runs the profile without being asked; SetIsDefault changes it. SetLabel renames the profile.
	IsDefault() bool
	SetIsDefault(bool)
	SetLabel(label string)
	// Tag and SupportsContinuousRun are those the profile was made with.
	Tag() string
	SupportsContinuousRun() bool
	// Dispose removes the profile.
	Dispose()
}

// CoverageCount is how many of the things of a kind were covered (VS Code's TestCoverageCount).
type CoverageCount struct{ Covered, Total int }

// FileCoverage is the coverage of a file (VS Code's FileCoverage): the statements, and optionally the branches and the declarations.
type FileCoverage struct {
	// URI is the file (file:///path).
	URI string
	// Statement is the statement coverage and Branch and Declaration the others, nil when not measured.
	Statement   CoverageCount
	Branch      *CoverageCount
	Declaration *CoverageCount
	// Data is the value LoadDetailedCoverage gets back, so the extension can keep its own handle on the file's data.
	Data any
}

// BranchCoverage is a branch of a statement (VS Code's BranchCoverage).
type BranchCoverage struct {
	// Executed is how many times the branch ran.
	Executed int
	// Location is where the branch is, nil for the statement's own.
	Location *Range
	// Label is the name of the branch.
	Label string
}

// StatementCoverage is a statement of a file and how many times it ran (VS Code's StatementCoverage).
type StatementCoverage struct {
	Executed int
	Location Range
	Branches []BranchCoverage
}

// DeclarationCoverage is a function or other declaration and how many times it ran (VS Code's DeclarationCoverage).
type DeclarationCoverage struct {
	Name     string
	Executed int
	Location Range
}

// CoverageDetails are the details of the coverage of one file.
type CoverageDetails struct {
	Statements   []StatementCoverage
	Declarations []DeclarationCoverage
}

// TestRun reports a run (VS Code's TestRun). It is made by TestController.CreateTestRun. After End its methods do nothing.
type TestRun interface {
	// Name is the name the run was made with.
	Name() string
	// Enqueued, Started and Skipped set the state of the test, and Passed, Failed and Errored its result; the duration is
	// TestNoDuration when it is not known. A test that has a result keeps it or takes one that is worse (VS Code's rule).
	Enqueued(item *TestItem)
	Started(item *TestItem)
	Skipped(item *TestItem)
	Passed(item *TestItem, duration time.Duration)
	Failed(item *TestItem, messages []TestMessage, duration time.Duration)
	Errored(item *TestItem, messages []TestMessage, duration time.Duration)
	// AppendOutput adds text to the run's output (ANSI sequences are kept). The place and the item are optional: with either, the output
	// can be jumped to from the test or the place.
	AppendOutput(text string, location *Location, item *TestItem)
	// AddCoverage reports the coverage of a file; reporting a file again replaces what was reported.
	AddCoverage(file FileCoverage)
	// End ends the run. A test still enqueued or started is marked skipped.
	End()
}

// TestController is the provider of a group of tests (VS Code's TestController).
type TestController interface {
	// ID is the id the controller was made with and Label its name; SetLabel renames it.
	ID() string
	Label() string
	SetLabel(label string)
	// Items are the top items of the controller's tree.
	Items() *TestItemCollection
	// CreateTestItem makes an item for the tree; it is in no tree until it is added.
	CreateTestItem(id, label, uri string) *TestItem
	// SetResolveHandler sets the function that loads the children of an item when it is expanded (the item has CanResolveChildren), and
	// of the whole tree with a nil item when the explorer is shown first.
	SetResolveHandler(fn func(ctx context.Context, item *TestItem) error)
	// SetRefreshHandler sets the function the Refresh Tests command calls.
	SetRefreshHandler(fn func(ctx context.Context) error)
	// CreateRunProfile makes a run profile. It fails with ExtensionTestingInvalid for an empty label, an unknown kind or no handler.
	CreateRunProfile(label string, kind TestRunKind, handler TestRunHandler, opts TestRunProfileOptions) (TestRunProfile, error)
	// CreateTestRun makes a run that reports to the results. A request the host gave a handler joins that request's result; any other
	// request starts a result of its own, which is kept in the saved results when persist is true. It fails with
	// ExtensionTestingInvalid for no request and ExtensionDeactivated after the activation ended.
	CreateTestRun(request *TestRunRequest, name string, persist bool) (TestRun, error)
	// InvalidateTestResults marks the results of the items (all, with none given) as outdated; the explorer shows them faded.
	InvalidateTestResults(items ...*TestItem)
	// Dispose removes the controller, its items and its profiles.
	Dispose()
}

// Tests is the Testing API of an extension in one scope.
type Tests interface {
	// CreateTestController makes a controller. It fails with ExtensionTestingInvalid for an empty id or label,
	// ExtensionTestingControllerTaken when the id is used, ExtensionTestingUnavailable in a scope with no testing service (a host without
	// a workbench) and ExtensionDeactivated after the activation ended. The controller is disposed with the activation.
	CreateTestController(id, label string) (TestController, error)
}
