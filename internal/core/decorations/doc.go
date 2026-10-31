// Package decorations is the file decorations service (SPEC §10.30 "File decorations and the Explorer", step 24.6): the colors, the
// badges and the tooltips that the Explorer, Open Editors, the editor tabs, the breadcrumbs, the Search results and the Source Control
// view show for a file. Providers (the Git extension, through api.Decorations()) answer for a URI; the service merges the answers
// of all providers, keeps them by URI, and tells the views which URIs changed.
//
// It is VS Code's DecorationsService with two differences. A provider here answers at once and from memory (VS Code lets it
// return a promise: a provider that has to ask a program fires the URI when it knows, see Registration.Fire), and the service builds
// no CSS: it hands the views a Decoration (the color ids in the order they apply, the badge, the tooltip) and each view draws it.
//
//   - The answers of the providers for one URI are sorted by weight, the highest first (the newest provider first among equal
//     weights); the tooltips are joined with " • ", the colors are a chain from which the first one the theme knows is used, the
//     text badges are joined with ", " and an icon badge wins over the text.
//   - A decoration with Bubble set shows up in every folder above the file: Get(folder, true) merges it into the folder, which
//     is then drawn in the color with a small dot as its badge (Decoration.Bubble) and the tooltip "Contains emphasized items".
//     Only the answers the service already has are bubbled: Fire a URI to have it asked, as the Git extension does for each file
//     of its status.
//   - The answers are cached by URI, also the answer "nothing", and Fire asks again. The cache is bounded (Options.MaxEntries):
//     when it is full the "nothing" answers are dropped first, and an answer that still does not fit is not kept.
//   - A provider that is slow (Options.SlowAfter, 1 ms) is logged, and after Options.SlowLimit slow answers it is not asked any
//     more for URIs it has not answered (the answers it gave stay; Fire or a new registration asks it again), so that a provider
//     that blocks cannot freeze the views.
//
// The service is safe for use from several goroutines; providers are called with its lock held and must not call it. Changes are
// announced to the subscribers (OnChange) from the goroutine that made them, after the lock is released.
//
// Ported from VS Code: src/vs/workbench/services/decorations/browser/decorationsService.ts and common/decorations.ts
package decorations
