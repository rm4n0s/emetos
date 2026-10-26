package langfeatures

import "context"

// CompletionResolver is a completion provider that fills in the details of an item when it is focused (completionItem/resolve,
// SPEC §9.12 "Completion"): the documentation, the detail and the additional edits are left out of the list and asked for
// the one item the user is looking at.
type CompletionResolver interface {
	// CanResolve tells whether ResolveCompletion would ask the provider for more about the item.
	CanResolve(item CompletionItem) bool
	// ResolveCompletion returns the item with what the provider added; ctx ends when the user moves to another row.
	ResolveCompletion(ctx context.Context, doc Document, item CompletionItem) (CompletionItem, error)
}

// CanResolveCompletion tells whether the provider that answered with the item can resolve it.
func (r *Registry) CanResolveCompletion(item CompletionItem) bool {
	rv, ok := item.source.(CompletionResolver)
	return ok && rv.CanResolve(item)
}

// ResolveCompletion asks the provider that answered with the item for the rest of it. An item whose provider cannot resolve
// is returned as it is. The error is the provider's.
func (r *Registry) ResolveCompletion(ctx context.Context, doc Document, item CompletionItem) (CompletionItem, error) {
	rv, ok := item.source.(CompletionResolver)
	if !ok || !rv.CanResolve(item) {
		return item, nil
	}
	return rv.ResolveCompletion(ctx, doc, item)
}

// MergeResolvedCompletion is the item with what its resolved copy has more: VS Code assigns the members the provider sent,
// and the ones a resolve can bring are the documentation, the detail, the additional edits and the command. The label, the
// text to insert and the ranges stay those of the list, which the sort and the insertion were worked out from.
func MergeResolvedCompletion(item, resolved CompletionItem) CompletionItem {
	if resolved.Documentation != nil {
		item.Documentation = resolved.Documentation
	}
	if resolved.Detail != "" {
		item.Detail = resolved.Detail
	}
	if resolved.AdditionalEdits != nil {
		item.AdditionalEdits = resolved.AdditionalEdits
	}
	if resolved.Command != nil {
		cmd := *resolved.Command
		if cmd.Owner == "" && item.Command != nil {
			cmd.Owner = item.Command.Owner
		}
		item.Command = &cmd
	}
	item.server = resolved.server
	return item
}
