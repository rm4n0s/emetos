#!/usr/bin/env bash
# Lists the error Tags of the editor (the first argument of errors.New, errors.NewErr and
# errors.NewErrf in the non-test code of cmd, internal, extension and extensions) and,
# with --untested, the Tags that no test asserts (CLAUDE.md "Definition of done",
# docs/IMPLEMENTATION.md step 9.9).
#
#   scripts/error-tags.sh               every Tag: Tag, file:line, function
#   scripts/error-tags.sh --untested    the Tags no test asserts; prints nothing when all are
#                                       tested, except those docs/ERROR-TAGS-UNTESTED.md lists
#   scripts/error-tags.sh --untested --all    the same, with the listed ones kept in
#
# A Tag counts as tested when a test asserts it in a route (HasRoute("...Func.Tag")), compares
# it through .Tag, or hands errors.Is a &errors.Error{Tag: ...}; internal/archtest/errortags.go
# says exactly how. A Tag that is not a string literal is an error. The exit status is 1 when
# --untested printed something or a Tag is not a literal. It needs no network and takes a second.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
# Emetos is cgo-free; building the tool without cgo is faster and needs no C compiler.
export CGO_ENABLED=0
exec go run ./internal/archtest/cmd/errortags "$@" "$root"
