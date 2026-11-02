package settings

// categoryTesting is the group of the settings of Testing.
const categoryTesting = "Testing"

// registerTesting registers the testing.* settings that the Testing views and the editor read: the count badge of the activity bar icon
// (step 26.2) and the settings of the editor, the peek, the Test Results and the runs (step 26.3). The coverage settings come with step
// 26.4. The names, the values and the defaults were checked against the pinned reference (decisions 1580 and 1581 list the differences from
// the SPEC).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/configuration.ts
func registerTesting(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "testing.countBadge", Types: []Type{String}, Default: "failed", Enum: []any{"failed", "off", "passed", "skipped"},
			EnumDescriptions: []string{"Show the number of failed tests", "Disable the testing count badge", "Show the number of passed tests",
				"Show the number of skipped tests"},
			Description: "Controls the count badge on the Testing icon on the Activity Bar.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.automaticallyOpenPeekView", Types: []Type{String}, Default: "never",
			Enum: []any{"failureAnywhere", "failureInVisibleDocument", "never"},
			EnumDescriptions: []string{"Open automatically no matter where the failure is.", "Open automatically when a test fails in a visible document.",
				"Never automatically open."},
			Description: "Configures when the error Peek view is automatically opened.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.showAllMessages", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether to show messages from all test runs.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.followRunningTest", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the running test should be followed in the Test Explorer view.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.defaultGutterClickAction", Types: []Type{String}, Default: "run", Enum: []any{"run", "debug", "runWithCoverage", "contextMenu"},
			EnumDescriptions: []string{"Run the test.", "Debug the test.", "Run the test with coverage.", "Open the context menu for more options."},
			Description:      "Controls the action to take when left-clicking on a test decoration in the gutter.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.gutterEnabled", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether test decorations are shown in the editor gutter.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.saveBeforeTest", Types: []Type{Boolean}, Default: true,
			Description: "Control whether save all dirty editors before running a test.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.openTesting", Types: []Type{String}, Default: "openOnTestStart",
			Enum: []any{"neverOpen", "openOnTestStart", "openOnTestFailure", "openExplorerOnTestStart"},
			EnumDescriptions: []string{"Never automatically open the testing views", "Open the test results view when tests start",
				"Open the test result view on any test failure", "Open the test explorer when tests start"},
			Description: "Controls when the testing view should open.", Scope: ScopeWindow, Category: categoryTesting,
		},
		Setting{
			Key: "testing.alwaysRevealTestOnStateChange", Types: []Type{Boolean}, Default: false,
			Description: "Always reveal the executed test when `#testing.followRunningTest#` is on. If this setting is turned off, only failed tests will be revealed.",
			Scope:       ScopeWindow, Category: categoryTesting,
		},
	)
}
