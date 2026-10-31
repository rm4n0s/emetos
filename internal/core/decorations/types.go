package decorations

// Data is what a provider says about a file (VS Code's IDecorationData).
type Data struct {
	// Weight orders the answers of several providers for a file, and the marks bubbled into a folder: the higher the weight the
	// earlier the answer comes, and the first color the theme knows is the color of the file.
	Weight int
	// Color is the id of a theme color the name of the file is drawn in (and its badge); empty leaves the color as it is.
	Color string
	// Letter is the text of the badge, one or two characters; Icon, the name of a Codicon, is a badge that wins over any letter.
	Letter, Icon string
	// Tooltip is the text of the hover; the tooltips of the providers are joined with " • ".
	Tooltip string
	// Strikethrough draws the name struck through.
	Strikethrough bool
	// Bubble shows the mark in the folders above the file too.
	Bubble bool
}

// Provider answers for the files. Decoration is called on the thread that asks (the UI thread, for the views) with the service's
// lock held: it must answer from memory, fast, and must not call the service. ok is false for a file the provider says nothing about.
type Provider interface {
	// Decoration is the decoration of the file, ok false for none.
	Decoration(uri string) (data Data, ok bool)
}

// ProviderFunc is a Provider that is a function.
type ProviderFunc func(uri string) (Data, bool)

// Decoration implements Provider.
func (f ProviderFunc) Decoration(uri string) (Data, bool) { return f(uri) }

// Decoration is what the views draw for a file (VS Code's IDecoration, without the CSS classes).
type Decoration struct {
	// Colors are the ids of the theme colors in the order they apply: the color of the name is the first one the theme knows.
	// Empty when no answer names a color (the name keeps its own color).
	Colors []string
	// Badge is the text at the end of the row: the letters of the answers joined with ", ". Empty for a folder with Bubble and
	// when an Icon is given.
	Badge string
	// Icon is the name of a Codicon drawn instead of the text badge.
	Icon string
	// Bubble says that the decoration is only what the files below the folder bubbled up: the views draw the small dot
	// (Codicon circle-small-filled, 40% opacity, in the color) instead of a badge.
	Bubble bool
	// Tooltip is the hover: the tooltips of the answers joined with " • ", or BubbleTooltip for a Bubble.
	Tooltip string
	// Strikethrough says that the name is drawn struck through.
	Strikethrough bool
}

// IsTextBadge says that Badge is a text to draw (VS Code's isTextBadge).
func (d Decoration) IsTextBadge() bool { return !d.Bubble && d.Icon == "" && d.Badge != "" }

// BubbleTooltip is the tooltip of a folder that shows only what its files bubbled.
const BubbleTooltip = "Contains emphasized items"

// TooltipSeparator joins the tooltips of several answers.
const TooltipSeparator = " • "
