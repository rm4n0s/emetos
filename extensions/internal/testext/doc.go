// Package testext holds the extensions that test the contract of package extension, and small examples of ones that use
// every part of the API. The first (New) uses the API of step 11.1: a command, a setting, a keybinding, a menu item and a status
// item. The second (NewTabs, step 17e.1) uses the tabs: an editor tab kind of which several are open at once, an editor title
// action, a window tab kind that keeps its state, and the events of other window tabs. They are not compiled into Emetos
// (extensions.All does not list them): they live under internal so that only the tests of the extensions package can use them.
package testext
