package debug

import "github.com/rm4n0s/emetos/internal/core/dap"

// AppendConsole writes a text to the Debug Console on behalf of an extension (vscode.debug.activeDebugConsole.append). It is an output
// event of the category "console" that belongs to no session; the Debug Console of step 25.6 shows it with the rest.
//
// Ported from VS Code: src/vs/workbench/api/common/extHostDebugService.ts (ExtHostDebugConsole)
func (s *Service) AppendConsole(text string) {
	s.model.emit(Event{Kind: EventOutput, Output: &dap.OutputEventBody{Category: "console", Output: text}})
}
