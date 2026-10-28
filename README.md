# Emetos

Emetos is a code editor in the spirit of Visual Studio Code, written in Go for the Linux desktop. Its interface is built on [Glutter](https://github.com/rm4n0s/glutter), a GUI toolkit for Go with Flutter's framework API. It is one Go binary: no Electron, no Node.js and no cgo.

It follows VS Code where VS Code has the behavior: the same settings, command ids, color themes, TextMate grammars, snippet files, `settings.json` and `keybindings.json` formats, and VS Code's Linux keyboard shortcuts, so a VS Code user finds their way. Language features (completion, hover, diagnostics, go to definition, rename and more) come from language servers, so the editor itself stays language-agnostic. Emetos has **its own extensions**: Go packages compiled into the program, not VS Code extensions (see [Extensions](#extensions)).

![A Go file open with the Explorer, tabs, breadcrumbs, the minimap and the status bar](docs/screenshots/editor.png)

## Status

**The last release is v0.1.0**, which holds areas 1 and 2 of [the specification](docs/SPEC.md) (the editor and the workbench). **`main` is ahead of it** and has no release of its own yet: this README describes `main`. A build of `main` calls itself by Go's version stamp (a pseudo-version), or `v0.1.0-dev` when it has none (`emetos --version`).

What is built (milestones M0 to M16 of [SPEC §18](docs/SPEC.md#18-milestones)):
- **The editor and the workbench** (areas 1 and 2, M0 to M10). [The coverage table](docs/IMPLEMENTED.md#coverage-of-areas-1-2-3-and-11) lists every row of areas 1, 2, 3 and 11 with its tests and what is partial.
- **Extensions** compiled into Emetos, events between them, and event sharing between window tabs (M11, [SPEC §10.15](docs/SPEC.md#1015-extensions-extension-extensions)).
- **Language intelligence through LSP** (area 3, M12, [SPEC §9.12](docs/SPEC.md#912-language-intelligence-area-3-corelsp-corelangfeatures-guieditor-guiviews-extension)).
- **The Go, Remote Work and Relay extensions** (M14 to M16).

Experimental: **Remote Work and Relay are new** and have been tested on one machine (instances on loopback, and a simulated lossy link), not yet on real networks. Remote Work's picture is not sharp on a HiDPI screen. Read [their security notes](#remote-work-and-relay) before you turn either on.

Planned, not built: project-wide search and replace, source control, run and debug through debug adapters (the Go extension has no debugger yet), the integrated terminal, VS Code's `tasks.json` tasks (the Tasks extension runs command lines you list), remote development (SSH remotes and containers), notebooks, the testing UI, multi-root workspaces and settings sync. Language servers other than `gopls` are not bundled (an extension can declare one). The `opencode` agent is not built.

## Build and install

You need **Go 1.27** or newer (`go version`) and `git`. Nothing else: there is no C code, so no C compiler, no system libraries and no `make` are needed to build (what Emetos needs at run time is under [Requirements](#requirements)).

### Build from source

```sh
git clone https://github.com/rm4n0s/emetos.git
cd emetos
go build -tags glutter_release -o emetos ./cmd/emetos
./emetos --version
./emetos .                      # open the current folder
```

`go build` downloads the Go modules Emetos uses the first time. The result is one binary, `./emetos` (about 90 MB), that you can copy anywhere, for example to `~/.local/bin/emetos`.

**Always build with `-tags glutter_release`** for daily use. `glutter_release` is a Go build tag: it compiles [Glutter](https://github.com/rm4n0s/glutter) in release mode, like Flutter's `--release` (`foundation.DebugMode` is `false`, so the framework's debug checks, Flutter's asserts, and its diagnostics are left out of the binary). Without the tag you get a **debug build**: the same program, but those checks make the interface several times slower, and the developer commands of the proofs (`dev.*`) are switched on. A debug build is for working on Emetos itself:

```sh
go build -o emetos ./cmd/emetos          # debug build
go run ./cmd/emetos .                    # build and run a debug build in one go
```

`CGO_ENABLED=0 go build -tags glutter_release -o emetos ./cmd/emetos` gives the same binary without cgo, which also works on a machine with no C compiler at all.

### Install with `go install`

```sh
go install -tags glutter_release github.com/rm4n0s/emetos/cmd/emetos@main
```

This builds `main`, which is what this README describes; `@latest` gives the release **v0.1.0**, which has only areas 1 and 2 (no language servers, no extensions, no Remote Work or Relay). From a checkout, `go install -tags glutter_release ./cmd/emetos` does the same. The program is put in `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is not set (`~/go/bin` by default); put that folder in your `PATH`.

### Update

```sh
cd emetos
git pull
go build -tags glutter_release -o emetos ./cmd/emetos
```

`emetos --version` prints the version (a release tag, or a pseudo-version with the commit for a build of `main`), the Go version and the platform.

### Requirements

- Linux with a **Wayland** or **X11** session.
- **Vulkan** or **OpenGL ES 3.0** (Glutter uses Vulkan and falls back to OpenGL ES).
- **fontconfig**, and a **monospace font**: the editor uses the desktop's monospace font and the workbench its UI font. Emetos bundles only its icon fonts.
- Optional: **xdg-desktop-portal** (with a backend for your desktop) for the Open Folder, Open File and Save As dialogs and for opening links. Without it, Emetos asks for the path in its own input box.
- Optional, for the **tray icon**: a StatusNotifier host. KDE Plasma and most panels have one. **GNOME shows no tray icons by itself**: install the AppIndicator extension (for example "AppIndicator and KStatusNotifierItem Support"). Without a tray, the close button of the window quits Emetos instead of hiding it.
- Optional, for the **Go** extension: **`gopls`** on your `PATH` (or its path in `go.goplsPath`), and the Go toolchain. The extension also uses `staticcheck` (lint) and `dlv` (for the debugger, which is not built yet). "Go: Install/Update Tools" in the command palette runs `go install` for them, only when you ask. Without `gopls`, Go files still have syntax highlighting, but no completion, hover or diagnostics.
- Optional, for **Remote Work** and **Relay**: when you turn them on, they listen on a port, so a firewall must let the other computer reach it: **Remote Work 47123 over TCP and UDP**, **Relay 47124 over TCP** (both can be changed on their pages). Nothing listens until you turn it on. Both need SSH public keys; see [the security notes](#remote-work-and-relay).
- Optional, for **Scan QR Code** (adding a public key with a camera): a camera that Linux shows as a V4L2 device (`/dev/video*`) that your user may open. Without one the button is disabled.

### Launcher entry and icon

`scripts/install-desktop.sh` makes Emetos appear in your application menu and launcher, with its icon. Run it from a checkout of this repository, after installing the program:

```sh
scripts/install-desktop.sh             # install
scripts/install-desktop.sh --uninstall # remove
```

It writes only two things under `${XDG_DATA_HOME:-~/.local/share}`: `applications/dev.emetos.Emetos.desktop` ([the entry](assets/dev.emetos.Emetos.desktop): `Exec=emetos %F`, text files and folders as its types) and the icon in every size of [`assets/icons`](assets/icons) as `icons/hicolor/<N>x<N>/apps/dev.emetos.Emetos.png`. It puts the absolute path of `emetos` in the entry, because a launcher often does not have `~/go/bin` in its `PATH` (`--exec <path>` says which program). It runs `update-desktop-database` and `gtk-update-icon-cache` when they are installed.

## Features

The feature list of [SPEC §3](docs/SPEC.md#3-areas), in short. The first two groups are v0.1.0's; the others came after it.

**Editor**
- Editing: typing, auto-indent and indentation rules of each language, auto-closing and surrounding brackets and quotes, word and word-part deletion, overtype mode (Insert key), indentation detection, tab size and spaces or tabs per file.
- Line commands: move, copy, delete, insert, join, transpose, sort, trim trailing whitespace, change case, line and block comments.
- Multiple cursors (Alt+Click, add next occurrence with Ctrl+D, select all occurrences, column selection), expand and shrink selection, cursor undo, the primary selection of X11 and Wayland.
- Find and replace with regular expressions, match case, whole word, find in selection and preserve case.
- Syntax highlighting with TextMate grammars and VS Code's color themes, for Go, JSON, YAML, Markdown, Python, JavaScript, TypeScript, HTML, CSS, SCSS, Less, XML, SQL, shell, Makefile, Dockerfile, INI, C, C++, Rust, diff, git files and Go module files.
- Bracket matching, bracket pair colorization and guides, indent guides, folding (by indentation and `#region`), minimap, sticky scroll, breadcrumbs.
- Snippets in VS Code's syntax (your own files, tab-completion) and Emmet abbreviations for HTML and CSS.
- Diff editor (side by side and inline, with character-level changes) and a three-way merge editor with accept incoming, current or both, usable from `git` through `--diff`, `--merge` and `--wait`.
- Encodings, LF and CRLF line endings, byte order marks, read-only files, files that change on disk, large files (a 200 MB file opens in seconds), and binary files (refused with a notice).
- Save, Save As, Save All, auto save, hot exit (unsaved changes, also of untitled files, come back after a restart or a crash), and an undo history per document.

**Workbench**
- Menus, activity bar, side bar, editor area, panel and status bar; each can be hidden and the sizes are remembered. Zen mode, full screen, zoom and a centered layout.
- Editor groups in a grid (split in any direction, move and copy editors, maximize), tabs with preview, pinned and dirty editors, drag and drop, and Ctrl+Tab to switch editors.
- Explorer with file watching, `files.exclude`, new, rename, delete to the trash, cut, copy, paste, drag and drop (also from your file manager), and an Open Editors section.
- Command palette (Ctrl+Shift+P), quick open (Ctrl+P) with fuzzy search, go to line (Ctrl+G), back and forward through your edit locations, reopen closed editor.
- Notifications, and the Output and Problems panels (Problems shows what language servers and tools report).
- An integrated terminal in the panel, as VS Code's: see [Terminal](#terminal).
- Color themes (the six VS Code defaults, your own in `themes/`, and following the desktop's dark or light mode) and the Seti file icon theme.
- A settings editor (Ctrl+,) and a keyboard shortcuts editor (Ctrl+K Ctrl+S), both editing the JSON files and keeping your comments, and profiles (named sets of settings, keybindings and snippets).
- The window comes back as you left it: layout, editors with their cursors, scroll positions and folds, and the open folders.

**Language intelligence** ([SPEC §9.12](docs/SPEC.md#912-language-intelligence-area-3-corelsp-corelangfeatures-guieditor-guiviews-extension)), from language servers over LSP 3.17 and from providers that an extension writes in Go. VS Code's commands, keys and settings:
- Diagnostics as squiggles and in the Problems panel, with F8 and Shift+F8 to step through them.
- Completion (the suggest widget with its details pane and snippets), hover, and signature help.
- Go to Definition, Type Definition, Implementations and References (F12, Shift+F12), with the peek view; document highlights.
- Symbols: the Outline view, symbols in the breadcrumbs and sticky scroll, Go to Symbol in the file (Ctrl+Shift+O) and in the workspace (Ctrl+T).
- Code actions (the light bulb, Ctrl+.), formatting (on demand, on save, on type and on paste), organize imports on save, and rename (F2) across files, with a preview.
- Code lens, inlay hints, semantic tokens, folding ranges and selection ranges from the server.
- The only server bundled is `gopls`, through the Go extension. Another language needs an extension that declares its server.

![Hover over a Go function, with its signature, documentation and a link, from gopls](docs/screenshots/go-hover.png)

## One window, workspace tabs and the tray

Emetos has one window. Every folder you open is a **workspace tab** in the strip at the top of the window (the folder name, a dot when it has unsaved changes, and a close button). Tabs switch with Ctrl+Alt+PageUp and Ctrl+Alt+PageDown, Ctrl+Alt+1 to 9, or a picker, and can be dragged to a new order. Tabs that are not shown keep running (their files, watchers and auto save).

`emetos <folder>` reaches the running Emetos and opens or focuses that folder's tab. When the window's close button is pressed and a tray is available, the window **hides to the tray icon** and Emetos keeps running; click the icon to bring it back, or use its menu to choose a tab, open a folder, or quit (`window.closeToTray` turns this off). The tabs, their editors and the window come back when you start Emetos again.

![Three workspace tabs in the strip at the top of the window](docs/screenshots/workspace-tabs.png)

![The picker that switches between workspace tabs](docs/screenshots/switch-picker.png)

## Extensions

Emetos's extensions are **Go packages compiled into the program** ([SPEC §10.15](docs/SPEC.md#1015-extensions-extension-extensions)). There is no plugin loading, no marketplace and no Node.js host, and VS Code extensions do not run in Emetos. Adding one is adding its folder under `extensions/` and one line to `extensions/all.go`, and building Emetos again. An extension that fails (an error or a panic) is switched off for that tab, with a notification and a log entry; the rest of Emetos goes on. The page **Extensions** (Ctrl+Shift+X) lists them, with their state and a Disable button (`extensions.disabled`).

An extension contributes any mix of **seven kinds of contribution**, each drawn inside the box Emetos gives it:
- **Window tab**: a tab in the strip beside the workspaces (a scratchpad, Remote Work, Relay).
- **Side bar view**: an activity bar icon with views (TODOs, Notes).
- **Panel view**: a tab of the bottom panel beside Problems and Output (Tasks, Go Tests).
- **Editor tab**: a tab in the editor area that is not a file (a note).
- **Editor title action**: a small icon at the right end of a tab strip (New Note).
- **File view**: micro-tabs inside a file's tab for the files an extension declares (the Markdown preview next to Text).
- **Contributions to the built-in parts**: Output channels, Problems markers, status bar items and notifications.

Beside these, an extension can add commands, keys, menu items, settings, language packs (grammars, themes, snippets) and language servers. Extensions talk to each other only through **events** (publish, subscribe, request and reply), and they only see the events of their own tab. **Event sharing**: events cross from one window tab to another only along a link that **you** allow, in one direction, asked in the receiving tab (Allow, This Session Only or Deny) and kept in `event-sharing.json`. The page **Extensions › Event Sharing** lists the links, removes them, and marks a workspace **private**, which sends nothing out. No settings file can create a link.

**What is bundled:**
- **Language packs**: the 22 languages of [SPEC §8.1](docs/SPEC.md#81-languages-corelanguage) (Go, JSON, YAML, Markdown, Python, JavaScript, TypeScript, HTML, CSS, SCSS, Less, XML, SQL, shell, Makefile, Dockerfile, INI, C and C++, Rust, diff, git files and Go modules).
- **TODOs**: a side bar view that lists the `TODO` and `FIXME` comments of the folder and opens them.
- **Terminal**: shells in the panel, below.
- **Tasks**: a panel tab that runs the command lines you list in `emetos.tasks.commands` in the folder, shows their output and puts their errors in Problems. (It is not VS Code's `tasks.json`.)
- **Markdown preview**: a Preview micro-tab for Markdown files, and Open Preview to the Side.
- **Notes**: notes for the workspace, with a list in the side bar and a note in an editor tab.
- **Scratchpad**: a window tab with notes that stay across restarts and a feed of the edits made in the other tabs (it shows them once you allow the link).
- **Go**, **Remote Work** and **Relay**, below.

![The TODOs view, the Tasks tab of the panel with the problems go vet found, and squiggles from gopls](docs/screenshots/todos-tasks.png)

### Terminal

The **Terminal** tab of the panel (Ctrl+` shows it, and hides the panel again while the terminal has the keyboard) runs your shell in a pseudo-terminal, with VS Code's commands, keys, settings (`terminal.integrated.*`) and colors (`terminal.*`). It is Emetos's own: an emulator written in Go and a grid drawn from a glyph atlas with two draw calls (no web view, and nothing of xterm.js), so that `cat` of a 100 MB file takes about as long as in a fast native terminal (`docs/PERFORMANCE.md`).
- **Several terminals.** Ctrl+Shift+` makes another, Ctrl+Shift+5 **splits** the one you are in (side by side, with a draggable divider), and the **tabs list** at the right shows when there are two or more (`terminal.integrated.tabs.*`): click a row to switch, middle-click or the trash button to kill, the secondary button for the menu. The shell starts in the folder of the workspace (`terminal.integrated.cwd`), from `terminal.integrated.defaultProfile.linux` or your `$SHELL`; the profiles are listed under the chevron.
- **Keys.** Every key goes to the program, except the commands of `terminal.integrated.commandsToSkipShell` (VS Code's list: Ctrl+P, Ctrl+Shift+P, Ctrl+Tab, F5 and so on stay Emetos's; add or take away with a `-`). Ctrl+Shift+C copies the selection, Ctrl+Shift+V pastes (as a bracketed paste when the program asked for it, with VS Code's warning for several lines), Shift+Insert pastes the primary selection, Shift+PageUp, Shift+Home and the mouse wheel scroll back, a double click selects a word and a triple click a line. Programs that track the mouse get it; hold Alt or Shift to select anyway.
- **Looks.** The font follows `editor.fontFamily` or `terminal.integrated.fontFamily`, true color, bold, italic, underline styles, wide characters and emoji, box-drawing and block characters drawn to fill the cell, the cursor styles, the scrollbar, and the colors of the theme.

![Two terminals side by side in the panel, with the tabs list at the right](docs/screenshots/terminal.png)

### Go

The **Go** extension ([SPEC §10.17](docs/SPEC.md#1017-the-go-extension-extensionsgolang)) starts `gopls` for each Go workspace (settings under `go.gopls`, **Go: Restart Language Server**) and adds: Go commands (add or remove struct tags, fill a struct, generate unit tests, toggle the test file, run), tests with run test lenses above test functions and a Go Tests panel, `go vet` and `staticcheck` or `golangci-lint` on save into Problems, and a status bar item with the Go version and the state of `gopls`. Debugging is not built.

## Remote Work and Relay

Two extensions that let one Emetos be used from another computer. **Both are new and experimental**, and both are **off until you turn them on**. Both log in over **SSH with public keys only** (no passwords): the computer that is used lists the keys that may log in.

**Remote Work** ([SPEC §10.18](docs/SPEC.md#1018-remote-work-remote-control-of-a-workspace-extensionsremotework)) shows **one workspace of computer A as a window tab on computer B**, and B works in it with its keyboard and mouse. A runs a second view of the workspace, so both see the same documents. On A, the page **Remote Work** turns it on for a workspace, sets the port (**47123**, TCP and UDP), the idle timeout (30 minutes) and the allowed keys. On B, the `+` menu has **Remote Work...**: host, port and a private key file or the SSH agent. The picture travels over QUIC (UDP) and falls back to SSH over TCP when UDP does not get through. Not built: sound, drag and drop between the computers, more than one workspace per session, and a sharp picture on a HiDPI screen (it is drawn at logical pixels).

![A Remote Work tab: the workspace of another computer, shown in a tab of this window (here both on one machine)](docs/screenshots/remote-work.png)

**Relay** ([SPEC §10.19](docs/SPEC.md#1019-relay-event-control-of-a-workspace-from-another-machine-extensionsrelay)) is event control, not a picture: nothing but events, requests and commands cross the network. In the **Relay** tab, *Be a Host* turns listening on (port **47124**, TCP), picks the workspaces to share and lists the keys, each **Watch only** (the default) or **Watch and send**. *Hosts* connects to other computers and shows an **Events** panel (a live log of a shared workspace) and, for Watch and send, a **Send** panel (publish an event, make a request, run a command). A client only acts in the workspace it was given, and commands that reach outside it (window tabs, quitting, other folders, profiles, user settings) are refused.

![The Relay tab: a connected host, the Events panel and the Send panel](docs/screenshots/relay.png)

### Security notes

Read these before you turn either extension on ([SPEC §10.18](docs/SPEC.md#1018-remote-work-remote-control-of-a-workspace-extensionsremotework) and [§10.19](docs/SPEC.md#1019-relay-event-control-of-a-workspace-from-another-machine-extensionsrelay) have the whole design):
- **A listed key gives that person your power in the workspace**: they can edit and delete files and run tasks (Remote Work, and Relay's Watch and send). List only keys you would trust with your account.
- **Relay's Watch only still sees all document text**: every event of the workspace, including the text and the edits of your documents.
- **Compare the fingerprint the first time.** The first connection shows the host's key fingerprint; compare it with the one on the host's page before you trust it. A connection is only as safe as that comparison. A changed host key is refused with a warning.
- **Over the internet, use a VPN.** The login is as safe as SSH with key-only login, and a VPN is better still. Do not expose the ports to the internet without one. Failed logins are limited to 5 a minute for each address, but that is not a defense by itself.
- You see every login: a banner in the workspace (with Disconnect), a notification, and a log entry. Turning the feature off or removing a key ends the sessions it logged in, and a Remote Work session also ends after its idle timeout.
- No settings file can turn either one on or add a key: only their own pages do. Emetos keeps no private key of yours: it remembers the path of your key file, reads the file when you connect, and asks a passphrase in a dialog without keeping it. The only private keys it keeps are its own host keys (`remote-work-host-key` and `relay-host-key`, mode 0600, see [Where things are](#where-things-are)).

## Command line

```
emetos [options] [folder] [file...]
```

A folder opens as a workspace tab and files open in the active tab. When Emetos is already running, the arguments are handed to it and the command ends.

| Option | Meaning |
|---|---|
| `-g`, `--goto file:line[:col]` | open the file with the cursor at the line (and column); may be repeated |
| `-d`, `--diff a b` | compare two files |
| `-m`, `--merge p1 p2 base result` | merge two versions of a file with their base into the result |
| `-w`, `--wait` | end only when the opened files (or the diff or merge editor) are closed, for `git commit` |
| `-n`, `--new-window` | open in a new tab |
| `-r`, `--reuse-window` | open in the existing tab |
| `--profile name` | use the profile (made empty when it does not exist) |
| `--log level` | log level: `debug`, `info`, `warn` or `error` (default `info`) |
| `-v`, `--version` | print the version and exit |
| `-h`, `--help` | print the help and exit |

Use `--` before a path that starts with a dash. The exit status is 0 on success, 1 on failure and 2 for a wrong command line.

## Where things are

Emetos follows the XDG base directories. `$XDG_CONFIG_HOME` is `~/.config` and `$XDG_STATE_HOME` is `~/.local/state` unless you set them.

| What | Where |
|---|---|
| User settings | `$XDG_CONFIG_HOME/emetos/settings.json` |
| Keybindings | `$XDG_CONFIG_HOME/emetos/keybindings.json` |
| Snippets | `$XDG_CONFIG_HOME/emetos/snippets/` (`<language>.json` and `*.code-snippets`) |
| Color themes (VS Code's JSON or `.tmTheme`) | `$XDG_CONFIG_HOME/emetos/themes/` |
| Workspace settings | `<folder>/.emetos/settings.json` (Emetos does not read `.vscode/`) |
| Other profiles | `$XDG_CONFIG_HOME/emetos/profiles/<name>/` (the same files) |
| Window state, recent lists, backups of unsaved work, logs | `$XDG_STATE_HOME/emetos/` |
| Trash (deleted files) | `$XDG_DATA_HOME/Trash` (the freedesktop.org home trash) |
| Socket of the running instance | `$XDG_RUNTIME_DIR/emetos-<uid>.sock` |
| Event sharing links and private workspaces | `$XDG_STATE_HOME/emetos/event-sharing.json` (mode 0600) |
| Remote Work, host side: its settings and allowed keys | `$XDG_STATE_HOME/emetos/remote-work.json` (mode 0600) |
| Remote Work, host side: this computer's SSH host key | `$XDG_STATE_HOME/emetos/remote-work-host-key` (mode 0600, made when its page first opens) |
| Remote Work, client side: the host keys you trusted | `$XDG_STATE_HOME/emetos/remote-work-hosts.json` (mode 0600) |
| Relay, host side: its settings, shared workspaces, allowed keys and their permissions | `$XDG_STATE_HOME/emetos/relay.json` (mode 0600) |
| Relay, host side: this computer's SSH host key (not Remote Work's) | `$XDG_STATE_HOME/emetos/relay-host-key` (mode 0600) |
| Relay, client side: the host keys you trusted | `$XDG_STATE_HOME/emetos/relay-hosts.json` (mode 0600) |

The files of the last seven rows are in the state folder of Emetos and are the same for every profile; the folder has mode 0700. Only their own pages write them, and **Emetos refuses a file that other users can write** (a private key also one they can read): it does not use it and does not replace it. The saved hosts of the Relay tab and the host and key file of a Remote Work tab are part of the window state. Deleting `remote-work-host-key` or `relay-host-key` makes a new identity, and every client must trust it again.

The files are JSON with comments, in VS Code's formats. "Preferences: Open User Settings (JSON)" and "Open Keyboard Shortcuts (JSON)" open them in Emetos.

## Privacy

Emetos sends nothing about you anywhere: **no telemetry, no crash reports and no update checks.** It uses the network only for what you ask of a feature, and these are the things that do:
- **Remote Work and Relay**: only while you have turned them on, and only with the computers whose keys you listed or whose hosts you saved.
- **Language servers and tools you install**: `gopls` and the `go` command may download modules as your Go settings say; "Go: Install/Update Tools" runs `go install` when you click it.
- **Links**: a link in a notification opens in your browser only when you click it.

Logs stay on your disk (`$XDG_STATE_HOME/emetos/logs`) and are shown in the Output panel. The camera is read only while the Scan QR Code dialog is open, and no picture of it is kept or sent.

## Screenshots

Every screenshot of this README is made by `scripts/screenshots.sh` (it uses `scripts/proof.sh` in a headless compositor, with isolated settings and state, from the fixtures in `testdata/screenshots/`; the Remote Work and Relay shots run two instances on `127.0.0.1`). The rest:

![The side by side diff editor](docs/screenshots/diff.png)

![The merge editor with the incoming and current sides over the result](docs/screenshots/merge.png)

![The settings editor with a search](docs/screenshots/settings.png)

## Development

[`CLAUDE.md`](CLAUDE.md) has the conventions and the checks, [`docs/SPEC.md`](docs/SPEC.md) is the specification, [`docs/IMPLEMENTATION.md`](docs/IMPLEMENTATION.md) the step-by-step plan, [`docs/IMPLEMENTED.md`](docs/IMPLEMENTED.md) the log of finished steps, and [`docs/PERFORMANCE.md`](docs/PERFORMANCE.md) the measurements. In short:

```sh
go build ./...                  # build (CGO_ENABLED=0 works)
go test -race ./...             # tests; no display or GPU needed
go vet ./... && golangci-lint run
scripts/fetch-reference.sh      # the pinned VS Code sources used for porting (git-ignored)
```

## License

Emetos is released under the MIT License ([`LICENSE`](LICENSE)). It bundles and ports material from other projects, notably Visual Studio Code (grammars, themes, the Seti icons and ported code, MIT), the Codicons icon font (CC BY 4.0: "Codicons by Microsoft Corporation, licensed under CC BY 4.0"), Emmet, vscode-textmate and vscode-go (MIT), and it links Go modules such as Glutter, `quic-go`, `x/crypto` and `gozxing`. [`NOTICE`](NOTICE) lists every asset with its license, and the license texts are in [`third_party/`](third_party/).
