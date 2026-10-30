// Package git is the Git client of the Git extension (SPEC §10.30 "Running git", D35): it finds the user's git program,
// runs it safely and portably, and parses what it prints. It is plain Go (the standard library and rm4n0s/errors), with no
// UI and no Emetos import, and it is tested against the real git in isolated repositories (extensions/gitscm/gittest) and
// against recorded outputs.
//
// Runner starts the commands: arguments as a list and never through a shell, a fixed environment, a process group per
// command that is stopped on cancellation, bounded or streamed output, the Git channel log and the classification of
// failures into the error tags of VS Code's GitErrorCodes. Queue orders the commands of one repository. The parsers
// (ParseStatus, ParseCommits, ParseRefs, ParseBlame and the others) read the output of status, log, for-each-ref, blame and
// the rest, as streams where git's format is a stream. Repo gathers the read commands of one repository; Init and Clone, on
// the Runner, are the only write commands of this step (the others are added by the steps that use them).
//
// Ported from VS Code: extensions/git/src/git.ts (the parsers, the error classification, the commands), operation.ts (the
// kinds of operation that the queue orders) and util.ts (splitInChunks, versions). VS Code branches on git versions down to
// 2.13; Emetos requires 2.30 (D35) and drops those branches. VS Code's cache.ts (an LRU cache for blame and history) is not
// ported: no step needs it yet.
package git
