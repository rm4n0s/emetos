package langserver

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// ProvideContent registers the provider of the read-only documents of a pack (the class files of jdtls) and returns its registration.
// A scope that cannot show documents (a view of a client, a tab of an extension) is no failure of the pack and says nothing that is worth
// a warning, because the log is read by people who have no tab to show it in: the answer is nil, and it is logged at debug level. what
// names the documents in that line ("the class files of jdtls"). Any other failure is logged as a warning and also answers nil, so that
// a pack goes on without its read-only tabs.
func ProvideContent(api extension.API, what string, p extension.ContentProvider) extension.ContentRegistration {
	reg, err := api.Documents().ProvideContent(p)
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ExtensionDocumentUnavailable" {
			api.Log().Debug(what+" are not available in this scope", "err", err)
			return nil
		}
		api.Log().Warn(what+" are not available", "err", err)
		return nil
	}
	return reg
}
