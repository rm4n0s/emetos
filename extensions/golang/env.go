package golang

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
)

// "Go: Show Go Environment" (SPEC §10.17 "As built (step 14.1)"): the output of `go env` in the Output channel "Go".

// showEnvironmentCommand is the command "Go: Show Go Environment".
func (g *golang) showEnvironmentCommand(context.Context, any) error {
	g.showEnvironment()
	return nil
}

// channel makes the Output channel on first use.
func (g *golang) channel() extension.OutputChannel {
	if g.env == nil {
		ch, err := g.api.Output().Channel(EnvironmentChannel)
		if err != nil {
			g.api.Log().Warn("the Output channel of the Go environment cannot be made", "err", err)
			return nil
		}
		g.env = ch
	}
	return g.env
}

// showEnvironment runs `go env` in the folder of the workspace, off the UI thread, and writes what it prints to the Output
// channel "Go", which is shown at once (the lines follow as they come). A go program that is not found or fails is reported there.
func (g *golang) showEnvironment() {
	ch := g.channel()
	if ch == nil {
		return
	}
	ch.Clear()
	ch.Show(true)
	program, err := g.goProgram()
	if err != nil {
		ch.AppendLine("The go command was not found. Install Go, or set go.alternateTools in the settings.")
		return
	}
	folder := g.api.Scope().Folder
	ch.AppendLine("$ go env")
	spec := cmdSpec{Program: program, Args: []string{"env"}, Dir: folder, Env: g.programEnv()}
	g.closers.Add(1)
	go func() {
		defer g.closers.Done()
		ctx, cancel := context.WithTimeout(g.ctx, envTimeout)
		defer cancel()
		err := runProgram(ctx, spec, ch.AppendLine)
		if err != nil {
			ch.AppendLine("go env failed: " + failureText(err, nil))
		}
	}()
}
