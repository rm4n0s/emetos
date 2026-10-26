//go:build emetos_claude

package claude

// realClaudeTests is true: TestMain leaves PATH and the environment alone so that TestRealClaude can find the user's
// own program.
const realClaudeTests = true
