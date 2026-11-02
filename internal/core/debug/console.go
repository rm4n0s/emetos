package debug

import "github.com/rm4n0s/emetos/internal/core/dap"

// AppendConsole writes a text to the Debug Console on behalf of an extension (vscode.debug.activeDebugConsole.append): it goes to
// the console of the focused session, in the color of a warning as VS Code writes it, or else of the session that started last. With
// no session at all there is no console and the text is dropped, as in VS Code. The event of the category "console" that belongs to
// no session is sent too, for the listeners of the model.
//
// Ported from VS Code: src/vs/workbench/api/browser/mainThreadDebugService.ts ($appendDebugConsole)
func (s *Service) AppendConsole(text string) {
	s.model.emit(Event{Kind: EventOutput, Output: &dap.OutputEventBody{Category: "console", Output: text}})
	sess := s.model.Focus().Session
	if sess == nil {
		if all := s.model.Sessions(true); len(all) > 0 {
			sess = all[len(all)-1]
		}
	}
	if sess != nil {
		sess.enqueueOutput(func() { sess.AppendToRepl(text, ReplWarning) })
	}
}

// replConfig is the settings of the Debug Console the models read.
func (s *Service) replConfig() ReplConfig {
	if s.opts.Console == nil {
		return DefaultReplConfig
	}
	return s.opts.Console()
}

// AddOutput writes text to the Debug Console of the session as an output event of the category would: "stdout" and "stderr" for what a
// program prints, "console" for the debugger's own words. It is what the lines of the standard output and error of an adapter that runs
// the program (Delve) become (step 25.7); it may be called from any goroutine, also before the adapter has answered. The text is a
// whole line when it ends in a line feed.
func (s *Session) AddOutput(category, text string) {
	if text == "" {
		return
	}
	s.handleOutput(&dap.OutputEventBody{Category: category, Output: text})
}
