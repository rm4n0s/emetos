package keybinding

import "strings"

// Key is a key of the keyboard, independent of the layout and of Glutter. The values and the
// names are VS Code's KeyCode: letters and digits are the keys of a US layout, punctuation
// is named by its US-layout key (SPEC §9.3).
//
// Ported from VS Code: src/vs/base/common/keyCodes.ts
type Key int

// The keys. Unknown is the zero value and stands for a key without a name.
const (
	Unknown Key = iota
	Backspace
	Tab
	Enter
	Shift
	Ctrl
	Alt
	PauseBreak
	CapsLock
	Escape
	Space
	PageUp
	PageDown
	End
	Home
	LeftArrow
	UpArrow
	RightArrow
	DownArrow
	Insert
	Delete
	Digit0
	Digit1
	Digit2
	Digit3
	Digit4
	Digit5
	Digit6
	Digit7
	Digit8
	Digit9
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ
	Meta
	ContextMenu
	F1
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12
	F13
	F14
	F15
	F16
	F17
	F18
	F19
	F20
	F21
	F22
	F23
	F24
	NumLock
	ScrollLock
	Semicolon
	Equal
	Comma
	Minus
	Period
	Slash
	Backquote
	BracketLeft
	Backslash
	BracketRight
	Quote
	OEM8
	IntlBackslash
	Numpad0
	Numpad1
	Numpad2
	Numpad3
	Numpad4
	Numpad5
	Numpad6
	Numpad7
	Numpad8
	Numpad9
	NumpadMultiply
	NumpadAdd
	NumpadSeparator
	NumpadSubtract
	NumpadDecimal
	NumpadDivide
	KeyInComposition
	ABNTC1
	ABNTC2
	AudioVolumeMute
	AudioVolumeUp
	AudioVolumeDown
	BrowserSearch
	BrowserHome
	BrowserBack
	BrowserForward
	MediaTrackNext
	MediaTrackPrevious
	MediaStop
	MediaPlayPause
	LaunchMediaPlayer
	LaunchMail
	LaunchApp2
	Clear
)

// keyCount is the number of keys, Unknown included.
const keyCount = int(Clear) + 1

// keyName holds the three spellings of a key: the label shown in the UI, and the names
// accepted in keybindings.json for a US keyboard and for other keyboards.
type keyName struct {
	ui      string
	us      string
	general string
}

// keyNames is indexed by Key. The names are those of the table in VS Code's keyCodes.ts.
var keyNames = [keyCount]keyName{
	Unknown:            {ui: "unknown", us: "", general: ""},
	Backspace:          {ui: "Backspace", us: "", general: ""},
	Tab:                {ui: "Tab", us: "", general: ""},
	Enter:              {ui: "Enter", us: "", general: ""},
	Shift:              {ui: "Shift", us: "", general: ""},
	Ctrl:               {ui: "Ctrl", us: "", general: ""},
	Alt:                {ui: "Alt", us: "", general: ""},
	PauseBreak:         {ui: "PauseBreak", us: "", general: ""},
	CapsLock:           {ui: "CapsLock", us: "", general: ""},
	Escape:             {ui: "Escape", us: "", general: ""},
	Space:              {ui: "Space", us: "", general: ""},
	PageUp:             {ui: "PageUp", us: "", general: ""},
	PageDown:           {ui: "PageDown", us: "", general: ""},
	End:                {ui: "End", us: "", general: ""},
	Home:               {ui: "Home", us: "", general: ""},
	LeftArrow:          {ui: "LeftArrow", us: "Left", general: ""},
	UpArrow:            {ui: "UpArrow", us: "Up", general: ""},
	RightArrow:         {ui: "RightArrow", us: "Right", general: ""},
	DownArrow:          {ui: "DownArrow", us: "Down", general: ""},
	Insert:             {ui: "Insert", us: "", general: ""},
	Delete:             {ui: "Del", us: "Delete", general: ""},
	Digit0:             {ui: "0", us: "", general: ""},
	Digit1:             {ui: "1", us: "", general: ""},
	Digit2:             {ui: "2", us: "", general: ""},
	Digit3:             {ui: "3", us: "", general: ""},
	Digit4:             {ui: "4", us: "", general: ""},
	Digit5:             {ui: "5", us: "", general: ""},
	Digit6:             {ui: "6", us: "", general: ""},
	Digit7:             {ui: "7", us: "", general: ""},
	Digit8:             {ui: "8", us: "", general: ""},
	Digit9:             {ui: "9", us: "", general: ""},
	KeyA:               {ui: "A", us: "", general: ""},
	KeyB:               {ui: "B", us: "", general: ""},
	KeyC:               {ui: "C", us: "", general: ""},
	KeyD:               {ui: "D", us: "", general: ""},
	KeyE:               {ui: "E", us: "", general: ""},
	KeyF:               {ui: "F", us: "", general: ""},
	KeyG:               {ui: "G", us: "", general: ""},
	KeyH:               {ui: "H", us: "", general: ""},
	KeyI:               {ui: "I", us: "", general: ""},
	KeyJ:               {ui: "J", us: "", general: ""},
	KeyK:               {ui: "K", us: "", general: ""},
	KeyL:               {ui: "L", us: "", general: ""},
	KeyM:               {ui: "M", us: "", general: ""},
	KeyN:               {ui: "N", us: "", general: ""},
	KeyO:               {ui: "O", us: "", general: ""},
	KeyP:               {ui: "P", us: "", general: ""},
	KeyQ:               {ui: "Q", us: "", general: ""},
	KeyR:               {ui: "R", us: "", general: ""},
	KeyS:               {ui: "S", us: "", general: ""},
	KeyT:               {ui: "T", us: "", general: ""},
	KeyU:               {ui: "U", us: "", general: ""},
	KeyV:               {ui: "V", us: "", general: ""},
	KeyW:               {ui: "W", us: "", general: ""},
	KeyX:               {ui: "X", us: "", general: ""},
	KeyY:               {ui: "Y", us: "", general: ""},
	KeyZ:               {ui: "Z", us: "", general: ""},
	Meta:               {ui: "Meta", us: "", general: ""},
	ContextMenu:        {ui: "ContextMenu", us: "", general: ""},
	F1:                 {ui: "F1", us: "", general: ""},
	F2:                 {ui: "F2", us: "", general: ""},
	F3:                 {ui: "F3", us: "", general: ""},
	F4:                 {ui: "F4", us: "", general: ""},
	F5:                 {ui: "F5", us: "", general: ""},
	F6:                 {ui: "F6", us: "", general: ""},
	F7:                 {ui: "F7", us: "", general: ""},
	F8:                 {ui: "F8", us: "", general: ""},
	F9:                 {ui: "F9", us: "", general: ""},
	F10:                {ui: "F10", us: "", general: ""},
	F11:                {ui: "F11", us: "", general: ""},
	F12:                {ui: "F12", us: "", general: ""},
	F13:                {ui: "F13", us: "", general: ""},
	F14:                {ui: "F14", us: "", general: ""},
	F15:                {ui: "F15", us: "", general: ""},
	F16:                {ui: "F16", us: "", general: ""},
	F17:                {ui: "F17", us: "", general: ""},
	F18:                {ui: "F18", us: "", general: ""},
	F19:                {ui: "F19", us: "", general: ""},
	F20:                {ui: "F20", us: "", general: ""},
	F21:                {ui: "F21", us: "", general: ""},
	F22:                {ui: "F22", us: "", general: ""},
	F23:                {ui: "F23", us: "", general: ""},
	F24:                {ui: "F24", us: "", general: ""},
	NumLock:            {ui: "NumLock", us: "", general: ""},
	ScrollLock:         {ui: "ScrollLock", us: "", general: ""},
	Semicolon:          {ui: ";", us: ";", general: "OEM_1"},
	Equal:              {ui: "=", us: "=", general: "OEM_PLUS"},
	Comma:              {ui: ",", us: ",", general: "OEM_COMMA"},
	Minus:              {ui: "-", us: "-", general: "OEM_MINUS"},
	Period:             {ui: ".", us: ".", general: "OEM_PERIOD"},
	Slash:              {ui: "/", us: "/", general: "OEM_2"},
	Backquote:          {ui: "`", us: "`", general: "OEM_3"},
	BracketLeft:        {ui: "[", us: "[", general: "OEM_4"},
	Backslash:          {ui: "\\", us: "\\", general: "OEM_5"},
	BracketRight:       {ui: "]", us: "]", general: "OEM_6"},
	Quote:              {ui: "'", us: "'", general: "OEM_7"},
	OEM8:               {ui: "OEM_8", us: "", general: ""},
	IntlBackslash:      {ui: "OEM_102", us: "", general: ""},
	Numpad0:            {ui: "NumPad0", us: "", general: ""},
	Numpad1:            {ui: "NumPad1", us: "", general: ""},
	Numpad2:            {ui: "NumPad2", us: "", general: ""},
	Numpad3:            {ui: "NumPad3", us: "", general: ""},
	Numpad4:            {ui: "NumPad4", us: "", general: ""},
	Numpad5:            {ui: "NumPad5", us: "", general: ""},
	Numpad6:            {ui: "NumPad6", us: "", general: ""},
	Numpad7:            {ui: "NumPad7", us: "", general: ""},
	Numpad8:            {ui: "NumPad8", us: "", general: ""},
	Numpad9:            {ui: "NumPad9", us: "", general: ""},
	NumpadMultiply:     {ui: "NumPad_Multiply", us: "", general: ""},
	NumpadAdd:          {ui: "NumPad_Add", us: "", general: ""},
	NumpadSeparator:    {ui: "NumPad_Separator", us: "", general: ""},
	NumpadSubtract:     {ui: "NumPad_Subtract", us: "", general: ""},
	NumpadDecimal:      {ui: "NumPad_Decimal", us: "", general: ""},
	NumpadDivide:       {ui: "NumPad_Divide", us: "", general: ""},
	KeyInComposition:   {ui: "KeyInComposition", us: "", general: ""},
	ABNTC1:             {ui: "ABNT_C1", us: "", general: ""},
	ABNTC2:             {ui: "ABNT_C2", us: "", general: ""},
	AudioVolumeMute:    {ui: "AudioVolumeMute", us: "", general: ""},
	AudioVolumeUp:      {ui: "AudioVolumeUp", us: "", general: ""},
	AudioVolumeDown:    {ui: "AudioVolumeDown", us: "", general: ""},
	BrowserSearch:      {ui: "BrowserSearch", us: "", general: ""},
	BrowserHome:        {ui: "BrowserHome", us: "", general: ""},
	BrowserBack:        {ui: "BrowserBack", us: "", general: ""},
	BrowserForward:     {ui: "BrowserForward", us: "", general: ""},
	MediaTrackNext:     {ui: "MediaTrackNext", us: "", general: ""},
	MediaTrackPrevious: {ui: "MediaTrackPrevious", us: "", general: ""},
	MediaStop:          {ui: "MediaStop", us: "", general: ""},
	MediaPlayPause:     {ui: "MediaPlayPause", us: "", general: ""},
	LaunchMediaPlayer:  {ui: "LaunchMediaPlayer", us: "", general: ""},
	LaunchMail:         {ui: "LaunchMail", us: "", general: ""},
	LaunchApp2:         {ui: "LaunchApp2", us: "", general: ""},
	Clear:              {ui: "Clear", us: "", general: ""},
}

// String is the name of the key in the UI, such as "K", "LeftArrow" or "OEM_102".
func (k Key) String() string {
	if k < 0 || int(k) >= keyCount {
		return keyNames[Unknown].ui
	}
	return keyNames[k].ui
}

// UserSettings is the name of the key in keybindings.json, in lower case: "k", "left", "[".
func (k Key) UserSettings() string {
	if k < 0 || int(k) >= keyCount {
		return ""
	}
	n := keyNames[k]
	if n.us != "" {
		return strings.ToLower(n.us)
	}
	return strings.ToLower(n.ui)
}

// IsModifier tells whether the key is Ctrl, Shift, Alt or Meta.
func (k Key) IsModifier() bool {
	return k == Ctrl || k == Shift || k == Alt || k == Meta
}

// uiKeys, userUSKeys and userGeneralKeys find a key by its lower-case name. Where two keys
// share a name, the one defined later wins, as in VS Code.
var uiKeys, userUSKeys, userGeneralKeys = buildKeyMaps()

func buildKeyMaps() (ui, us, general map[string]Key) {
	ui = make(map[string]Key, keyCount)
	us = make(map[string]Key, keyCount)
	general = make(map[string]Key, keyCount)
	for i := range keyCount {
		n := keyNames[i]
		k := Key(i)
		usName := n.us
		if usName == "" {
			usName = n.ui
		}
		generalName := n.general
		if generalName == "" {
			generalName = usName
		}
		ui[strings.ToLower(n.ui)] = k
		us[strings.ToLower(usName)] = k
		general[strings.ToLower(generalName)] = k
	}
	return ui, us, general
}

// KeyFromString finds a key by the name the UI shows ("K", "LeftArrow"), ignoring case.
// It returns Unknown when there is no such key.
func KeyFromString(name string) Key {
	return uiKeys[strings.ToLower(name)]
}

// KeyFromUserSettings finds a key by a name of keybindings.json ("k", "left", "oem_minus"),
// ignoring case. It returns Unknown when there is no such key.
func KeyFromUserSettings(name string) Key {
	lower := strings.ToLower(name)
	if k := userUSKeys[lower]; k != Unknown {
		return k
	}
	return userGeneralKeys[lower]
}
