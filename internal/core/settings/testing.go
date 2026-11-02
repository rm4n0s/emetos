package settings

// categoryTesting is the group of the settings of Testing.
const categoryTesting = "Testing"

// registerTesting registers the testing.* settings that the Test Explorer reads (step 26.2, SPEC §10.32): the count badge of the activity
// bar icon. The settings of the editor, the peek, the run and the coverage come with steps 26.3 and 26.4. The name, the values and the
// default were checked against the pinned reference.
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
	)
}
