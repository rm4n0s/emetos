# Emetos

Emetos is a code editor in the spirit of Visual Studio Code, written in Go for the Linux desktop. Its interface is built on [Glutter](https://github.com/rm4n0s/glutter), a GUI toolkit for Go with Flutter's framework API. It is one Go binary: no Electron, no Node.js and no cgo.

It follows VS Code where VS Code has the behavior: the same settings, command ids, color themes, TextMate grammars, snippet files, `settings.json` and `keybindings.json` formats, and VS Code's Linux keyboard shortcuts, so a VS Code user finds their way. Language features (completion, hover, diagnostics, go to definition, rename and more) come from language servers, so the editor itself stays language-agnostic. Emetos has **its own extensions**: Go packages compiled into the program, not VS Code extensions (see [Extensions](#extensions)).

![A Go file open with the Explorer, tabs, breadcrumbs, the minimap and the status bar](docs/screenshots/editor.png)

## Status

**The last release is v0.1.0**, which holds areas 1 and 2 of [the specification](docs/SPEC.md) (the editor and the workbench). **`main` is ahead of it** and has no release of its own yet: this README describes `main`. A build of `main` calls itself by Go's version stamp (a pseudo-version), or `v0.1.0-dev` when it has none (`emetos --version`).

What is built (milestones M0 to M27 of [SPEC §18](docs/SPEC.md#18-milestones); M23 only removed an extension):
- **The editor and the workbench** (areas 1 and 2, M0 to M10). [The coverage table](docs/IMPLEMENTED.md#coverage-of-areas-1-2-3-and-11) lists every row of areas 1, 2, 3 and 11 with its tests and what is partial.
- **Extensions** compiled into Emetos, events between them, and event sharing between window tabs (M11, [SPEC §10.15](docs/SPEC.md#1015-extensions-extension-extensions)).
- **Language intelligence through LSP** (area 3, M12, [SPEC §9.12](docs/SPEC.md#912-language-intelligence-area-3-corelsp-corelangfeatures-guieditor-guiviews-extension)).
- **The Go and Remote Work extensions** (M14, M15).
- **Language servers of more languages** (M24b, [SPEC §10.34](docs/SPEC.md#1034-language-servers-of-more-languages-the-language-packs)): Rust (rust-analyzer), C and C++ (clangd), Python (basedpyright, Pyright or python-lsp-server), TypeScript and JavaScript (typescript-language-server), Java (Eclipse JDT LS) and C# (csharp-ls), found and started the way `gopls` is, and never installed by Emetos. See [Languages](#languages).
- **Containers and databases** (M18 to M22): the **Containers** (Docker) and **Podman** extensions ([SPEC §10.20](docs/SPEC.md#1020-containers-docker-and-podman-extensionscontainers-extensionspodman)), and the database extensions **SQL** (PostgreSQL, MySQL, MariaDB, SQLite and SQL Server, [SPEC §10.21](docs/SPEC.md#1021-sql-databases-extensionssql)), **MongoDB** ([§10.22](docs/SPEC.md#1022-mongodb-extensionsmongodb)) and **Redis** ([§10.23](docs/SPEC.md#1023-redis-extensionsredis)), which share the **Databases** view of the activity bar ([§10.24](docs/SPEC.md#1024-what-the-database-and-container-extensions-share-and-the-clients-of-a-hosted-workspace)). **Connect to Database** on a container opens the connection form of its database, and clients of a hosted workspace cannot run the commands of these six extensions.
- **Git and Source Control** (area 5, M24, [SPEC §10.30](docs/SPEC.md#1030-git-and-source-control-extensionsgitscm)): VS Code's Git, with the Source Control view, diffs, the quick diff in the gutter, commit, branches, merge and rebase, remotes, stash, worktrees, the Graph, the Timeline and blame. See [Git](#git); [the coverage table of area 5](docs/IMPLEMENTED.md#coverage-of-area-5-source-control-spec-3-1030) lists every row with its tests.
- **Run and Debug** (area 6, M25, [SPEC §10.31](docs/SPEC.md#1031-run-and-debug-internalcoredap-internalcoredebug-internalguiviewsdebug-the-language-extensions)): VS Code's debug workbench over the Debug Adapter Protocol, with debuggers for Go (Delve), Python (debugpy), C, C++ and Rust (gdb 14 or newer, lldb-dap), and JavaScript and TypeScript (js-debug), found or fetched on your click and never installed by Emetos. See [Run and Debug](#run-and-debug); [the coverage table of area 6](docs/IMPLEMENTED.md#coverage-of-area-6-run-and-debug-spec-3-1031) lists every row with its tests.
- **Testing** (the testing UI of area 10, M26, [SPEC §10.32](docs/SPEC.md#1032-testing-internalcoretesting-internalguiviewstesting-the-language-extensions)): VS Code's Testing workbench (the Test Explorer, the glyphs and failures in the editor, the peek, Test Results and Test Coverage) with test providers for Go (`go test`), Python (pytest and unittest), Rust (`cargo test`), JavaScript and TypeScript (Node's test runner, Jest and Vitest) and C and C++ (CTest), run from the tools you installed and never installed by Emetos. See [Testing](#testing); [the coverage table of the testing UI](docs/IMPLEMENTED.md#coverage-of-area-10-the-testing-ui-spec-3-1032) lists every row with its tests.
- **HTTP Client** (area 12, M27, [SPEC §10.33](docs/SPEC.md#1033-http-client-extensionshttpclient)): collections of requests in readable files of the project (`.emetos/http/`, shared through git), a request tab, a response pane, environments and variables with secrets in the desktop keyring, extraction and checks without scripts, a collection runner with data files, and OAuth 2.0 (client credentials, and authorization code with PKCE). See [HTTP Client](#http-client); [the coverage table of area 12](docs/IMPLEMENTED.md#coverage-of-area-12-the-http-client-spec-3-1033) lists every row with its tests.

Experimental: **Remote Work is new** and has been tested on one machine (instances on loopback, and a simulated lossy link), not yet on real networks. Remote Work's picture is not sharp on a HiDPI screen. Read [its security notes](#remote-work) before you turn it on.

Also new: **the container and database extensions** have been tested against fake engines and servers and, one engine at a time, against Docker, Podman and throwaway PostgreSQL, MySQL, MariaDB, MongoDB and Redis servers (SQL Server only against fakes), not against every version of each. Read [their security notes](#containers-and-databases) before you connect them to anything that matters.

Also new: **Git** has been tested against the real `git` (2.56) in temporary repositories, with remotes that are local bare repositories and a local HTTP server, and not against every version of `git` from 2.30 on. A push over SSH with a key that has a passphrase and your agent, and a fetch through your own credential helper, have not been tried on a real remote (`docs/MANUAL-CHECKS.md` lists them for the author). Read [its security notes](#git) before you open a repository you did not make.

Also new: **Run and Debug** has been tested against a scripted fake adapter and, one debugger at a time, against Delve v1.27.2, debugpy 1.8.22, gdb 18.1, lldb-dap and js-debug v1.140.0 on small programs, headless; not against every version of each debugger, and not yet by hand on a real GPU (`docs/MANUAL-CHECKS.md` lists it for the author). A debugger runs your program with your permissions, so debug only code you would run. Read [what is not built](#what-is-not-built-in-run-and-debug) before you rely on it.

Also new: **Testing** has been tested against fake runners, the real `go` and, one tool at a time, pytest, cargo, Node with Jest and Vitest, and CMake's ctest on small projects, headless; not against every version of each tool, and not yet by hand on a real GPU (`docs/MANUAL-CHECKS.md` lists it for the author). A test run executes your code with your permissions, so run only tests you would run. Read [what is not built](#what-is-not-built-in-testing) before you rely on it.

Also new: **the HTTP Client** has been tested against `httptest` servers and a fake authorization server on `127.0.0.1`, with the keyring through a fake, headless; not against real services, not against the real keyring or an OAuth provider, and not yet by hand on a real GPU (`docs/MANUAL-CHECKS.md` lists them for the author). A request goes where you point it, with the credentials you give it, so send only requests you would send. Read [what is not built](#what-is-not-built-in-the-http-client) before you rely on it.

Planned, not built: VS Code's `tasks.json` tasks (the Tasks extension runs command lines you list), remote development (SSH remotes and containers), notebooks, multi-root workspaces and settings sync. Emetos installs no language server (see [Languages](#languages)). The `opencode` agent is not built. The [Planned](#planned) section lists what comes next.

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

This builds `main`, which is what this README describes; `@latest` gives the release **v0.1.0**, which has only areas 1 and 2 (no language servers, no extensions, no Remote Work). From a checkout, `go install -tags glutter_release ./cmd/emetos` does the same. The program is put in `$(go env GOBIN)`, or `$(go env GOPATH)/bin` when `GOBIN` is not set (`~/go/bin` by default); put that folder in your `PATH`.

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
- Optional, for the **Go** extension: **`gopls`** on your `PATH` (or its path in `go.goplsPath`), and the Go toolchain. The extension also uses `staticcheck` (lint) and `dlv` (the debugger of [Run and Debug](#run-and-debug)). "Go: Install/Update Tools" in the command palette runs `go install` for them, only when you ask. Without `gopls`, Go files still have syntax highlighting, but no completion, hover or diagnostics.
- Optional, for the language servers of [Languages](#languages): the server of each language you work in (rust-analyzer, clangd, basedpyright, Pyright or python-lsp-server, typescript-language-server with `node` and `typescript`, jdtls with Java 21 or newer, csharp-ls with the .NET SDK). Emetos finds the one you installed; without it the language still has syntax highlighting, and a notification says how to install the server.
- Optional, for the **Git** extension: **`git` 2.30 or newer** on your `PATH` (or its path in `git.path`); `git stash push --staged` needs 2.35. Nothing is downloaded: without `git`, or with an older one, the Source Control view says so and does nothing else. For `git` over SSH, your own SSH keys and agent, and for HTTPS your own credential helper (`credential.helper`), as in a terminal.
- Optional, for the **Containers** extension: **Docker** (its engine running and its socket reachable by your user, or `containers.host` / `DOCKER_HOST` / a Docker context pointing at an engine), and the `docker` program for Build, Push, Compose and Open Shell. Optional, for **Podman**: **`podman`** on your `PATH` (Emetos asks a running Podman socket first and otherwise starts `podman system service` itself, on a private socket, nothing to enable in systemd). Without an engine the views say so and do nothing else.
- For the **databases** there is **nothing to install**: the drivers for PostgreSQL, MySQL, MariaDB, SQLite and SQL Server, and the MongoDB and Redis clients, are part of the program (pure Go, no C library). You need a server to connect to (SQLite only a file). Optional: a **Secret Service** keyring (GNOME Keyring, KWallet or another that implements `org.freedesktop.secrets`) to keep a database password between sessions; without one the password is asked when you connect.
- Optional, for **Remote Work**: when you turn it on, it listens on a port, so a firewall must let the other computer reach it: **47123 over TCP and UDP** (it can be changed on its page). Nothing listens until you turn it on. It needs SSH public keys; see [the security notes](#remote-work).
- Optional, for **Scan QR Code** (adding a public key with a camera): a camera that Linux shows as a V4L2 device (`/dev/video*`) that your user may open. Without one the button is disabled.

### Launcher entry and icon

`scripts/install-desktop.sh` makes Emetos appear in your application menu and launcher, with its icon. Run it from a checkout of this repository, after installing the program:

```sh
scripts/install-desktop.sh             # install
scripts/install-desktop.sh --uninstall # remove
```

It writes only two things under `${XDG_DATA_HOME:-~/.local/share}`: `applications/dev.emetos.Emetos.desktop` ([the entry](assets/dev.emetos.Emetos.desktop): `Exec=emetos %F`, text files and folders as its types) and the icon in every size of [`assets/icons`](assets/icons) as `icons/hicolor/<N>x<N>/apps/dev.emetos.Emetos.png`. It puts the absolute path of `emetos` in the entry, because a launcher often does not have `~/go/bin` in its `PATH` (`--exec <path>` says which program). It runs `update-desktop-database` and `gtk-update-icon-cache` when they are installed.

Without a shell, `emetos --install-launcher` writes the same entry and icons itself (its own absolute path, found with `os.Executable`) and exits; `emetos --uninstall-launcher` removes them. Neither runs a command, so a desktop picks up the new or removed files under `$XDG_DATA_HOME` on its own, a little later.

## Features

The feature list of [SPEC §3](docs/SPEC.md#3-areas), in short. The first two groups are v0.1.0's; the others came after it.

**Editor**
- Editing: typing, auto-indent and indentation rules of each language, auto-closing and surrounding brackets and quotes, word and word-part deletion, overtype mode (Insert key), indentation detection, tab size and spaces or tabs per file.
- Line commands: move, copy, delete, insert, join, transpose, sort, trim trailing whitespace, change case, line and block comments.
- Multiple cursors (Alt+Click, add next occurrence with Ctrl+D, select all occurrences, column selection), expand and shrink selection, cursor undo, the primary selection of X11 and Wayland.
- Find and replace with regular expressions, match case, whole word, find in selection and preserve case.
- Syntax highlighting with TextMate grammars and VS Code's color themes, for Go, JSON, YAML, Markdown, Python, JavaScript, TypeScript, HTML, CSS, SCSS, Less, XML, SQL, shell, Makefile, Dockerfile, INI, C, C++, Rust, Java, C#, diff, git files and Go module files.
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
- Run and Debug, as VS Code's: breakpoints, the debug toolbar, Variables, Watch, Call Stack, the Debug Console and `launch.json`: see [Run and Debug](#run-and-debug).
- Color themes (the six VS Code defaults, your own in `themes/`, and following the desktop's dark or light mode) and the Seti file icon theme.
- A settings editor (Ctrl+,) and a keyboard shortcuts editor (Ctrl+K Ctrl+S), both editing the JSON files and keeping your comments, and profiles (named sets of settings, keybindings and snippets).
- The window comes back as you left it: layout, editors with their cursors, scroll positions and folds, and the open folders.

**Search** ([SPEC §10.27](docs/SPEC.md#1027-search-internalcoresearch-extensionssearch))
- Find and replace in the files of the folder, as VS Code's Search view does: Ctrl+Shift+F (seeded with your selection) and Ctrl+Shift+H, with Match Case (Alt+C), Match Whole Word (Alt+W), regular expressions (Alt+R) and Preserve Case (Alt+P), and the boxes "files to include" and "files to exclude".
- The results show while you type, as a list or a tree of folders (`search.defaultViewMode`), with F4 and Shift+F4 to open the next and the previous match. `files.exclude`, `search.exclude` and your `.gitignore` files are honored, and the text of an editor with unsaved changes is searched instead of its file.
- Replace one match, one file or everything (Ctrl+Alt+Enter, after a question); an open file changes in one undo step, and a click on a match while the replace box has text shows the file with its replacements in the diff editor. Find in Folder... is in the Explorer's context menu.
- The engine is Emetos's own Go code (no `rg`), and it searches a generated tree of 20,000 files and 500 MB faster than `grep -rF` ([docs/PERFORMANCE.md](docs/PERFORMANCE.md)). Extensions reach it with `api.Workspace().FindTextInFiles`.

**Git and Source Control** (the Git extension; [Git](#git) below; [SPEC §10.30](docs/SPEC.md#1030-git-and-source-control-extensionsgitscm))
- The **Source Control view** (Ctrl+Shift+G, with a count badge on its icon): the Merge Changes, Staged Changes and Changes groups (untracked files in a group of their own with `git.untrackedChanges`) as a list or a tree, with the letters of git (`M A D R U !`) in color, one box for the commit message and one action button (Commit, Sync Changes, Publish Branch, Continue), and the actions Open Changes, Open File, Stage, Unstage and Discard on every row. Discard always asks first.
- **Diffs**: a click opens the diff editor of a change with VS Code's titles (`loader.go (Working Tree)`, `(Index)`), and the **multi-file diff editor** shows many files in one scrolling tab (the changes of a group, a commit, a stash).
- The **quick diff**: stripes in the gutter (and the overview ruler and the minimap) for the lines added, changed and deleted against the index; a click or Alt+F3 opens the **peek** under the change with **Stage Change** and **Revert Change**, and Alt+F5 goes to the next change. Stage or revert a selection of lines with the commands of the same names.
- **Commit** with Ctrl+Enter, **amend**, **sign off**, an empty commit, undo the last commit, smart commit, and the editor (`COMMIT_EDITMSG`) when the box is empty; hooks run as in a terminal.
- **Branches and tags** (the branch item in the status bar opens the checkout picker, with the branches, remote branches and tags), create, rename and delete, **merge, rebase, cherry-pick and revert**, and the **merge editor** for conflicts; **stash** (push, pop, apply, drop, view); **worktrees**.
- **Remotes**: fetch, pull, push, **sync**, publish a branch, add and remove a remote, and autofetch (off until you say yes). **Clone**, **Initialize Repository** and **Open Repository**; with several repositories (also a nested one or a submodule) the view lists them.
- The **Graph** under the Source Control view (the commits of the branch as lanes, merges, branch, remote branch and tag badges, incoming and outgoing changes, a search, and a menu to check out, branch, tag or cherry-pick a commit), the **Timeline** under the Explorer (the commits that changed the file of the editor), and **blame** (the commit of the line of the cursor after its text and in the status bar).
- **File decorations**: the letters and colors of git in the Explorer, Open Editors, the tabs, the breadcrumbs and the Search results, with a dot on the folders that contain changes.
- Passwords and passphrases that git asks for, and the question of `ssh` about a new host, open **in the window**, also when you type `git push` in the integrated terminal.

![The Source Control view with a change in every group and a commit message in the box, and the diff of a changed file](docs/screenshots/git-changes.png)

**Containers and databases** (extensions; [Containers and databases](#containers-and-databases) below, and [What is bundled](#extensions))
- **Docker and Podman**: a view of the containers (grouped by Compose project or pod), images, networks, volumes and contexts, with start, stop, logs in the panel, inspect, shells, build, Compose and push, and Add Docker Files to Workspace.
- **Databases**: one **Databases** view (the cylinder in the activity bar) for SQL, MongoDB and Redis connections. A **SQL** editor with the results in a panel, MongoDB **playgrounds** (`.mongodb`), and a Redis **key editor** and console.
- **Connect to Database** on a container of `postgres`, `mysql`, `mariadb`, `mongo`, `redis` or `valkey` opens the connection form filled from its published port and environment (never a password).

**Language intelligence** ([SPEC §9.12](docs/SPEC.md#912-language-intelligence-area-3-corelsp-corelangfeatures-guieditor-guiviews-extension)), from language servers over LSP 3.17 and from providers that an extension writes in Go. VS Code's commands, keys and settings:
- Diagnostics as squiggles and in the Problems panel, with F8 and Shift+F8 to step through them.
- Completion (the suggest widget with its details pane and snippets), hover, and signature help.
- Go to Definition, Type Definition, Implementations and References (F12, Shift+F12), with the peek view; document highlights.
- Find All References and Find All Implementations (Shift+Alt+F12, and the right-click menu of a method or a variable): the results fill a References view in the side bar, by file, with F4 and Shift+F4 to walk them and a history of the searches ([SPEC §10.28](docs/SPEC.md#1028-references-extensionsreferences)).
- Symbols: the Outline view, symbols in the breadcrumbs and sticky scroll, Go to Symbol in the file (Ctrl+Shift+O) and in the workspace (Ctrl+T).
- Code actions (the light bulb, Ctrl+.), formatting (on demand, on save, on type and on paste), organize imports on save, and rename (F2) across files, with a preview.
- Code lens, inlay hints, semantic tokens, folding ranges and selection ranges from the server.
- Servers come from the language packs, never from Emetos itself: `gopls` through the Go extension, and rust-analyzer, clangd, basedpyright (or Pyright or python-lsp-server), typescript-language-server, Eclipse JDT LS and csharp-ls through the packs of their languages, each one found where you installed it (see [Languages](#languages)). Another language needs an extension that declares its server.

![Hover over a Go function, with its signature, documentation and a link, from gopls](docs/screenshots/go-hover.png)

## One window, workspace tabs and the tray

Emetos has one window. Every folder you open is a **workspace tab** in the strip at the top of the window (the folder name, a dot when it has unsaved changes, and a close button). Tabs switch with Ctrl+Alt+PageUp and Ctrl+Alt+PageDown, Ctrl+Alt+1 to 9, or a picker, and can be dragged to a new order. Tabs that are not shown keep running (their files, watchers and auto save).

`emetos <folder>` reaches the running Emetos and opens or focuses that folder's tab. When the window's close button is pressed and a tray is available, the window **hides to the tray icon** and Emetos keeps running; click the icon to bring it back, or use its menu to choose a tab, open a folder, or quit (`window.closeToTray` turns this off). The tabs, their editors and the window come back when you start Emetos again.

![Three workspace tabs in the strip at the top of the window](docs/screenshots/workspace-tabs.png)

![The picker that switches between workspace tabs](docs/screenshots/switch-picker.png)

## Extensions

Emetos's extensions are **Go packages compiled into the program** ([SPEC §10.15](docs/SPEC.md#1015-extensions-extension-extensions)). There is no plugin loading, no marketplace and no Node.js host, and VS Code extensions do not run in Emetos. Adding one is adding its folder under `extensions/` and one line to `extensions/all.go`, and building Emetos again. An extension that fails (an error or a panic) is switched off for that tab, with a notification and a log entry; the rest of Emetos goes on. The page **Extensions** (Ctrl+Shift+X) lists them, with their state and a Disable button (`extensions.disabled`).

An extension contributes any mix of **seven kinds of contribution**, each drawn inside the box Emetos gives it:
- **Window tab**: a tab in the strip beside the workspaces (Remote Work).
- **Side bar view**: an activity bar icon with views (Search, References, Containers, Podman, Databases).
- **Panel view**: a tab of the bottom panel beside Problems and Output (Tasks, the container logs, the SQL and MongoDB results, the Redis console).
- **Editor tab**: a tab in the editor area that is not a file (the Remote Work page, a database connection form, a Redis key).
- **Editor title action**: a small icon at the right end of a tab strip (Open Preview to the Side).
- **File view**: micro-tabs inside a file's tab for the files an extension declares (the Markdown preview next to Text).
- **Contributions to the built-in parts**: Output channels, Problems markers, status bar items and notifications.

Beside these, an extension can add commands, keys, menu items, settings, language packs (grammars, themes, snippets) and language servers. Extensions talk to each other only through **events** (publish, subscribe, request and reply), and they only see the events of their own tab. **Event sharing**: events cross from one window tab to another only along a link that **you** allow, in one direction, asked in the receiving tab (Allow, This Session Only or Deny) and kept in `event-sharing.json`. The page **Extensions › Event Sharing** lists the links, removes them, and marks a workspace **private**, which sends nothing out. No settings file can create a link.

**What is bundled:**
- **Language packs**: the 22 languages of [SPEC §8.1](docs/SPEC.md#81-languages-corelanguage) (Go, JSON, YAML, Markdown, Python, JavaScript, TypeScript, HTML, CSS, SCSS, Less, XML, SQL, shell, Makefile, Dockerfile, INI, C and C++, Rust, diff, git files and Go modules).
- **Search**: VS Code's Search view, find and replace in the files of the folder, above.
- **Terminal**: shells in the panel, below.
- **Git**: VS Code's Git and the Source Control it is built on: the Source Control view, the Graph and the Timeline, drawn by Emetos and filled by the extension, below.
- **Tasks**: a panel tab that runs the command lines you list in `emetos.tasks.commands` in the folder, shows their output and puts their errors in Problems. (It is not VS Code's `tasks.json`.) A debug configuration's `preLaunchTask` and `postDebugTask` run these tasks (see [Run and Debug](#run-and-debug)).
- **Containers** (Docker): a side bar view of your Docker containers (grouped by Compose project), images, networks, volumes and contexts, with Start, Stop, Restart, Pause, Kill, View Logs (in the panel), Inspect, Open in Browser, Run Command, Explore Files, Run, Run Interactive, Pull, Tag, Push and Remove, Open Shell (in the Terminal), Build Image and Compose (Up, Up with chosen services, Down, Restart, Logs; also on a Compose project of the view) through your `docker` program, and Add Docker Files to Workspace (Go, Node.js, Python, .NET or a plain image); the Dockerfile and Compose language servers are used when they are on your `PATH`. It talks to the engine's socket (`containers.host`, `DOCKER_HOST`, your Docker context). **Access to the Docker socket is power over the computer** (whoever can use it can read and change any file as root), and Emetos asks first, with the count and the size, before it removes or prunes anything. [SPEC §10.20](docs/SPEC.md#1020-containers-docker-and-podman-extensionscontainers-extensionspodman).
- **Podman**: the same view and actions for Podman (the extension "Podman", its own icon in the activity bar), with what only Podman has: **pods** (the containers grouped under their pod, a Pods view with Start, Stop, Restart, Pause, Remove and Prune, Create Pod, and Create Pod from Selected Containers), **connections** (`podman system connection list`, rootless and rootful, and a pick of the engine to use), **Generate Kube YAML** (`podman kube generate`, opened as an untitled YAML editor), **Play Kube** and **Kube Down** (`podman kube play` and `down`, which asks first), and **Generate Quadlet** (a `.container` unit for systemd opened as an untitled editor: Emetos writes nothing to `~/.config/containers/systemd` and never runs `systemctl`). Build, Push, Compose (`podman compose` or `podman-compose`) and Open Shell go through your `podman` program. When no Podman socket answers, Emetos starts a `podman system service` of its own on a private socket and stops it when it ends, so nothing has to be enabled in systemd. [SPEC §10.20](docs/SPEC.md#1020-containers-docker-and-podman-extensionscontainers-extensionspodman).
- **References**: VS Code's references view. Find All References and Find All Implementations list the places a language server finds, by file, in a side bar view with a history of the searches.
- **Markdown preview**: a Preview micro-tab for Markdown files, and Open Preview to the Side.
- **Go** and **Remote Work**, below.
- **Databases**: the shared part of the three database extensions: the **Databases** view (an activity bar icon) with the connections of every engine, the connection form, `db-connections.json` and the results grid. [SPEC §10.24](docs/SPEC.md#1024-what-the-database-and-container-extensions-share-and-the-clients-of-a-hosted-workspace).
- **SQL**: PostgreSQL, MySQL, MariaDB, SQLite and SQL Server (pure Go drivers, nothing to install), their schema in the Databases view, SQL editors and a results panel, below.
- **MongoDB**: MongoDB connections, a browser for databases, collections, indexes and schemas, and **playgrounds** (`.mongodb` files), below.
- **Redis** (and Valkey and compatible servers): connections (URL, ACL user, TLS, Sentinel, Cluster, a database number, read-only), a browser for the keys, a key editor, a console, Pub/Sub, Monitor and server information, below.

![The Tasks tab of the panel with the problems go vet found, and squiggles from gopls](docs/screenshots/tasks.png)

The Notes, TODOs and Scratchpad extensions of earlier versions are gone. What you wrote in a note or in the Scratchpad is saved once, at the first start, as Markdown files in `$XDG_DATA_HOME/emetos/saved-notes/` (`~/.local/share/emetos/saved-notes/` by default), and a notification offers **Open Folder** ([SPEC §10.26](docs/SPEC.md#1026-removed-notes-todos-and-scratchpad)).

### Terminal

The **Terminal** tab of the panel (Ctrl+` shows it, and hides the panel again while the terminal has the keyboard) runs your shell in a pseudo-terminal, with VS Code's commands, keys, settings (`terminal.integrated.*`) and colors (`terminal.*`). It is Emetos's own: an emulator written in Go and a grid drawn from a glyph atlas with two draw calls (no web view, and nothing of xterm.js), so that `cat` of a 100 MB file takes about as long as in a fast native terminal (`docs/PERFORMANCE.md`).
- **Several terminals.** Ctrl+Shift+` makes another, Ctrl+Shift+5 **splits** the one you are in (side by side, with a draggable divider), and the **tabs list** at the right shows when there are two or more (`terminal.integrated.tabs.*`): click a row to switch, middle-click or the trash button to kill, the secondary button for the menu. The shell starts in the folder of the workspace (`terminal.integrated.cwd`), from `terminal.integrated.defaultProfile.linux` or your `$SHELL`; the profiles are listed under the chevron.
- **Keys.** Every key goes to the program, except the commands of `terminal.integrated.commandsToSkipShell` (VS Code's list: Ctrl+P, Ctrl+Shift+P, Ctrl+Tab, F5 and so on stay Emetos's; add or take away with a `-`). Ctrl+Shift+C copies the selection, Ctrl+Shift+V pastes (as a bracketed paste when the program asked for it, with VS Code's warning for several lines), Shift+Insert pastes the primary selection, Shift+PageUp, Shift+Home and the mouse wheel scroll back, a double click selects a word and a triple click a line. Programs that track the mouse get it; hold Alt or Shift to select anyway.
- **Find, links and commands.** Ctrl+F searches the scrollback (case, whole word, regular expression, F3 and Shift+F3, Enter in the box goes to the previous match as in VS Code). Web addresses, `path:line:col` file names (under the workspace folder) and OSC 8 hyperlinks are underlined under the pointer and open with Ctrl+click. **Run Selected Text** and **Run Active File** send the editor's text or file to the terminal, **Send Sequence**, **Rename** and **Select Default Profile** work as in VS Code, and Ctrl+Shift+F with text selected in the terminal searches the workspace for it in the Search view.
- **In the editor area.** *Move Terminal into Editor Area* (and *Create New Terminal in Editor Area*, or `terminal.integrated.defaultLocation` set to `editor`) makes a terminal a tab beside your files; drag a row of the tabs list onto the editor area (the middle of it, a tab strip, or an edge to split) for the same. It is the same shell and the same screen, and *Move Terminal into Panel* sends it back. Closing the tab ends the shell; Ctrl+W does it from the keyboard.
- **The tabs list.** Drag a row to put the tab elsewhere in the list, hover for the process, its id and its folder, *Change Icon...*, *Change Color...*, *Rename...*, *Join Terminals...*, *Unsplit Terminal*, *Kill All Terminals* and *Toggle Size to Content Width* (Alt+Z: a wide terminal that scrolls sideways) are in the right-click menu and the palette.
- **Sessions come back.** When Emetos ends, each terminal's name, icon, color, folder and last 100 lines are kept in the window's state file (only you can read it: mode 0600; it holds whatever your programs printed), and the next start shows them again, a line saying *History restored*, and a new shell in the same folder. The old processes end with Emetos. `terminal.integrated.enablePersistentSessions` (or `persistentSessionReviveProcess` set to `never`) turns it off, and `persistentSessionScrollback` sets the lines.
- **Git.** Git asks for its passwords here as it does for the Source Control view: with `git.terminalAuthentication` (on) a terminal that starts gets git's `GIT_ASKPASS`, so `git push` typed in it opens the password box of the window instead of asking on the terminal's own line (Escape leaves git to ask in the terminal; a terminal that was already running keeps what it had). `git.terminalGitEditor` (off) does the same for the editor git starts. See [Git](#git).
- **Looks.** The font follows `editor.fontFamily` or `terminal.integrated.fontFamily`, true color, bold, italic, underline styles, wide characters and emoji, box-drawing and block characters drawn to fill the cell, the cursor styles, the scrollbar, and the colors of the theme.
- **IntelliSense.** With shell integration (bash, zsh, fish or pwsh, on by default), **Ctrl+Space** opens a list of what you can type at the cursor, as VS Code's terminal does: the commands of your `PATH` and the shell's builtins, the subcommands and options of the commands Emetos knows (`git`, `npm`, `go`, `ls`, `tar`, `curl` and about 45 more), files and folders, and lines of your history. Down, Up, Tab and Enter choose and accept, Escape closes. To have it open as you type, set `terminal.integrated.suggest.quickSuggestions` (`commands` and `arguments` to `"on"`), as in VS Code; `terminal.integrated.suggest.enabled` turns it off. It reads files and folders only and never runs a program to find its answers.

![Two terminals side by side in the panel, with the tabs list at the right](docs/screenshots/terminal.png)

### Go

The **Go** extension ([SPEC §10.17](docs/SPEC.md#1017-the-go-extension-extensionsgolang)) starts `gopls` for each Go workspace (settings under `go.gopls`, **Go: Restart Language Server**) and adds: Go commands (add or remove struct tags, fill a struct, generate unit tests, toggle the test file, run), tests with run test lenses above test functions and Go's tests in the Testing view (a Test Explorer with packages, files, tests and subtests, failures with their messages in the editor, and coverage), `go vet` and `staticcheck` or `golangci-lint` on save into Problems, and a status bar item with the Go version and the state of `gopls`. **Debugging** is in [Run and Debug](#run-and-debug).

## Languages

Emetos gets language features (completion, hover, problems, go to definition, rename, code actions, formatting, inlay hints and more) from **language servers**, as VS Code does ([SPEC §10.34](docs/SPEC.md#1034-language-servers-of-more-languages-the-language-packs)). **Emetos never installs a server**: it looks for the program you installed (the path setting of the pack, then `PATH`, then the places the server's own installer uses), starts it for each folder, and shows its state in the status bar. When it cannot find one, a notification says so, with **How to Install** (a dialog with the command for your system, and a button that opens this section), **Copy Install Command** (the command goes to the clipboard; Emetos never runs it) and **Set Path...** (saves the path in your settings). Nothing here is downloaded, and no server with terms to accept is used (no Pylance, no C# Dev Kit).

| Language | Server | How to install it | The settings that matter |
|---|---|---|---|
| Go | `gopls` (see [Go](#go)) | `go install golang.org/x/tools/gopls@latest` | `go.goplsPath`, `go.gopls` |
| Rust | [rust-analyzer](https://rust-analyzer.github.io/book/installation.html) | `sudo dnf install rust-analyzer` (Fedora), `sudo apt install rust-analyzer` (Debian, Ubuntu), `sudo pacman -S rust-analyzer` (Arch), or `rustup component add rust-analyzer` | `rust-analyzer.server.path`, `rust-analyzer.check.command` (`check` or `clippy`), `rust-analyzer.cargo.features`, `rust-analyzer.procMacro.enable` |
| C and C++ | [clangd](https://clangd.llvm.org/installation) | `sudo dnf install clang-tools-extra` (Fedora), `sudo apt install clangd` (Debian, Ubuntu), `sudo pacman -S clang` (Arch), `sudo zypper install clang-tools` (openSUSE) | `clangd.path`, `clangd.arguments`, `clangd.fallbackFlags`; it reads `compile_commands.json` in the folder or in `build/` |
| Python | [basedpyright](https://docs.basedpyright.com/latest/installation/command-line-and-language-server/) (the default), [pyright](https://microsoft.github.io/pyright/#/installation) or [python-lsp-server](https://github.com/python-lsp/python-lsp-server#installation) | `pipx install basedpyright`; Pyright: `npm install -g pyright` (Arch: `sudo pacman -S pyright`); pylsp: `sudo dnf install python3-lsp-server` (Fedora), `sudo apt install python3-pylsp` (Debian, Ubuntu), `sudo pacman -S python-lsp-server` (Arch), `sudo zypper install python3-python-lsp-server` (openSUSE), else `pipx install python-lsp-server` | `python.languageServer` (`basedpyright`, `pyright`, `pylsp` or `none`), `basedpyright.path`, `pyright.path`, `pylsp.path`, `python.defaultInterpreterPath`, `basedpyright.analysis.typeCheckingMode`; "Python: Select Interpreter" in the Command Palette |
| TypeScript and JavaScript | [typescript-language-server](https://github.com/typescript-language-server/typescript-language-server#installing) (needs `node`) | `npm install -g typescript-language-server typescript@6` | `typescript.languageServer.path`, `typescript.nodePath`, `typescript.tsdk`, `typescript.preferences.*`, `typescript.format.*`; "TypeScript: Select TypeScript Version..." in the Command Palette |
| Java | [Eclipse JDT Language Server](https://github.com/eclipse-jdtls/eclipse.jdt.ls#installation) (`jdtls`, needs Java 21 or newer) | `sudo pacman -S jdtls` (Arch), or `mkdir -p ~/.local/share/jdtls && curl -fsSL https://download.eclipse.org/jdtls/snapshots/jdt-language-server-latest.tar.gz \| tar -xz -C ~/.local/share/jdtls` | `java.jdt.ls.path`, `java.jdt.ls.java.home`, `java.import.maven.enabled`, `java.import.gradle.enabled`, `java.configuration.updateBuildConfiguration` |
| C# | [csharp-ls](https://github.com/razzmatazz/csharp-language-server#quick-start) (needs the .NET SDK) | `dotnet tool install --global csharp-ls` | `csharp.languageServer.path`, `csharp.solution`; "C#: Select Solution" in the Command Palette |

- **The status bar item** of each language ("rust-analyzer", "clangd", "Java", "C#", the version of TypeScript, the Python interpreter) shows whether the server is starting, ready, failing or missing, and its menu has Restart, Show Output (the server's own messages; set `<pack>.trace.server` to `verbose` for the traffic), Open Settings and the entries of the language. A change of a setting that the server read at its start restarts it.
- **Settings that name a program are yours alone**: every path setting above (`rust-analyzer.server.path`, `clangd.path`, `basedpyright.path`, `pyright.path`, `pylsp.path`, `typescript.languageServer.path`, `java.jdt.ls.path`, `csharp.languageServer.path`), `clangd.arguments`, `java.jdt.ls.java.home`, `typescript.nodePath`, `typescript.tsdk` and `python.defaultInterpreterPath` are read from your user settings only, because they name a program Emetos runs; a folder's `.emetos/settings.json` cannot set them. In a workspace you host with Remote Work, they are the host's settings.
- **Rust**: the server's Run and Debug lenses run `cargo run` and `cargo test` in a terminal for now; commands for expanding a macro, the parent module, `Cargo.toml` and the syntax tree. **C and C++**: Alt+O switches between source and header; the status item says when there is no `compile_commands.json` (CMake writes one with `-DCMAKE_EXPORT_COMPILE_COMMANDS=ON`). **Python**: Emetos lists the `.venv`, `venv` and `env` of the folder and the interpreters on `PATH`, keeps your choice per folder and hands it to the server; Organize Imports. **TypeScript and JavaScript**: the TypeScript version is the one installed beside the server, unless you choose the folder's own `node_modules/typescript` ("Use Workspace Version": Emetos asks once and never takes it by itself, because it is code the folder brought). **Java**: Maven and Gradle projects are imported by the server (an import may need the build tool and the network); classes from jars open as read-only documents. **C#**: with several solutions in a folder Emetos asks which one to load; the sources of .NET libraries are decompiled into read-only documents.
- **Tested** against rust-analyzer 2026-10-26, clangd 22, basedpyright 1.40.2, Pyright 1.1.414, python-lsp-server 1.15.0, typescript-language-server 6.0.2 with TypeScript 6.0.3 and 5.9.3, jdtls 1.63.0 and csharp-ls 0.28.0 (.NET SDK 10.0), each on a small project in `testdata/`, with a temporary home folder and no network, and at the desktop only headless so far (`docs/MANUAL-CHECKS.md` lists the pass on a real GPU). The speed of each, from the start of the editor to its first problems, and of typing with it running, is in [`docs/PERFORMANCE.md`](docs/PERFORMANCE.md#language-servers-step-24b10).
- **Not built**: tests for Java and C# (the other languages' tests are in [Testing](#testing); debugging is in [Run and Debug](#run-and-debug)), F# and Kotlin, Java's Maven and Gradle views, Jupyter notebooks, `cpptools`' own IntelliSense and the C# Dev Kit.

![rust-analyzer: completion at a blank line of a Rust file, with type hints on the let bindings and a Run lens above main](docs/implemented-proof-images/24b.2-rust.png)

![clangd: a red squiggle under a call to a function that does not exist, parameter name hints and the status item "clangd"](docs/implemented-proof-images/24b.3-cpp.png)

![Python: Select Interpreter lists the .venv of the folder and the system's python3, over a file with problems from basedpyright](docs/implemented-proof-images/24b.4-python.png)

![TypeScript: Select TypeScript Version offers the global TypeScript 6.0.3 and the folder's own 5.9.3, with the notification that asks once](docs/implemented-proof-images/24b.5-typescript.png)

![jdtls: the completion of a Java expression, with the problems of the compiler of Eclipse](docs/implemented-proof-images/24b.6-java.png)

![csharp-ls: the members of a List<string> after a dot, with the problems of the compiler](docs/implemented-proof-images/24b.7-csharp.png)

## Git

The **Git** extension ([SPEC §10.30](docs/SPEC.md#1030-git-and-source-control-extensionsgitscm)) is VS Code's built-in Git, ported: its commands (about 180, all in the Command Palette under "Git:"), ids, settings (`git.*`, `scm.*`), menus, keys and texts. Emetos runs **your `git` program**, as VS Code does; there is no Git library inside it. The Source Control view, the Graph, the Timeline, the quick diff and the file decorations belong to the workbench, and the extension only says what git reports and what to run, so a second source control could be added without a change of the window (SPEC D34). There is **no GitHub extension**: no sign-in, no GitHub API and no "Publish to GitHub"; Git works with any remote (GitHub's too) through `git`, your credential helpers and SSH.

### How to use it

1. **Open a folder that is a repository** (or has one inside it). The first time, Emetos asks whether you trust the repository (see [the security notes](#security-notes-of-git)) and runs no `git` before your answer. A folder with no repository shows the buttons **Initialize Repository** and **Clone Repository**; **Git: Clone** is also in the palette.
2. **Source Control** (Ctrl+Shift+G, or the icon of the activity bar): the groups of changes, the commit box and the action button. Click a file for its diff; the row's buttons open the file, stage, unstage and discard (Discard asks first). **Ctrl+Enter** in the box commits what is staged; the dropdown of the button has Commit (Amend), Commit (Signed Off) and the other variants. With `git.enableSmartCommit` on, a commit with nothing staged commits all changes. The `...` menu of the view has Pull, Push, Fetch, the branch, remote, stash and tag submenus and **Show Git Output**.
3. **In the editor**, the stripes in the gutter show what changed since the index: green added, blue changed, a red triangle for deleted lines. Click one, or press Alt+F3 and Shift+Alt+F3, for the **peek** with the change as a small diff and the buttons Stage Change and Revert Change. Alt+F5 and Shift+Alt+F5 move to the next and the previous change. Ctrl+K Ctrl+Alt+S stages the selected lines.
4. **The status bar** has the branch (click: the checkout picker, with "Create new branch...", the branches, the remote branches and the tags), the sync item with the commits to pull and to push, and the blame of the line of the cursor. Remote commands (**Fetch**, **Pull**, **Push**, **Sync Changes**, **Publish Branch**) show progress in the view, and a rejected push offers **Pull**.
5. **The Graph** (the view under Changes) shows the commits of the current branch, its upstream and the merge base as lanes, with the badges of branches, remote branches and tags and the rows "Outgoing Changes" and "Incoming Changes". Click a commit for its changes; its menu checks it out, makes a branch or a tag, or cherry-picks it. The title of the view filters by branch (Auto, All, or a pick) and its box searches messages, authors and hashes. **The Timeline** (the view under Outline in the Explorer) lists the commits that touched the file of the editor; click one for that change of the file, or use Select for Compare and Compare with Selected. **Blame**: **Git: Toggle Git Blame Editor Decoration** shows the commit of each cursor's line after its text (`git.blame.editorDecoration.enabled`), and the status bar item shows it by default.
6. **Merge, rebase, cherry-pick and conflicts**: while one is in progress the branch item, the box and the action button say so (Continue), the **Merge Changes** group lists the unmerged files, and with `git.mergeEditor` on a click opens the merge editor; Abort is in the palette. **Stash**, **worktrees** (**Git: Create Worktree...**; `git.detectWorktrees`) and tags are in the palette and the `...` menu.
7. **Passwords.** When git asks for a username, a password, an SSH passphrase or "Are you sure you want to continue connecting?", a box opens in the window, also for a `git push` typed in the integrated terminal. Escape cancels, and the command fails with a notification that offers **Retry**. `git commit` and `git rebase -i` with no message open their file as an editor tab, and finish when you close it.

![The Graph under the Source Control view: lanes, a merge, and the badges of a branch, a remote branch and a tag](docs/screenshots/git-graph.png)

![The quick diff: stripes in the gutter for the lines added, changed and deleted, and the peek of one change with Stage Change and Revert Change](docs/screenshots/git-diff.png)

![The Timeline of a file under the Explorer](docs/screenshots/git-timeline.png)

![Blame: the commit of the line of the cursor after its text and in the status bar](docs/screenshots/git-blame.png)

### The settings that matter

All the settings of VS Code's git extension are there with their defaults (the settings editor lists them under Features > Source Control, and the **Git** channel of the Output panel logs every command). These are the ones to know:

| Setting | Default | What it does |
|---|---|---|
| `git.path` | none | where `git` is (a path, or a list of candidates); otherwise the first `git` of `PATH` |
| `git.enableSmartCommit` | `false` | commit all changes when nothing is staged (`git.suggestSmartCommit` asks first, `git.smartCommitChanges` says all or only tracked) |
| `git.autofetch` | `false` | `true` or `all` runs `git fetch` every `git.autofetchPeriod` seconds (180) while the window has the focus; see [Privacy](#privacy) |
| `git.blame.editorDecoration.enabled` | `false` | the commit of the cursor's line after its text (`git.blame.editorDecoration.template`, `git.blame.editorDecoration.disableHover`); `git.blame.statusBarItem.enabled` (on, `git.blame.statusBarItem.template`) is the status bar item; `git.blame.ignoreWhitespace` is `-w` |
| `git.autorefresh` | `true` | refresh the status when files change |
| `git.enabled` | `true` | switch the extension off |
| `git.confirmSync` | `true` | ask before Sync Changes pulls and pushes |
| `git.mergeEditor` | `false` | open the merge editor for a conflicted file, not the text editor |
| `git.untrackedChanges` | `mixed` | untracked files in Changes, or in a group of their own |
| `git.terminalAuthentication` | `true` | `git push` in the terminal asks in the window |
| `git.useIntegratedAskPass` | `true` | git's password questions open in the window |
| `scm.defaultViewMode` | `list` | the changes as a list or a tree |
| `scm.diffDecorations` | `all` | where the quick diff draws: `gutter`, `overview`, `minimap`, `all` or `none` |
| `scm.graph.badges`, `scm.graph.pageSize` | `filter`, `50` | the ref badges of the Graph and the commits per page |

### Requirements

`git` **2.30 or newer** (the commands that need a newer one say so: Stash Staged needs 2.35). **Emetos downloads nothing**; without `git`, or with an older one, the view says "Git is not installed, or is older than 2.30" and the Git commands stay out of the palette. Emetos runs `git` with a fixed environment (English messages, no pager, no prompt on the terminal) so that the same text means the same on every computer. The window never waits for `git` on its UI thread, and a repository with 100,000 changed files does not freeze it (`docs/PERFORMANCE.md`).

### Security notes of Git

- **A repository can name programs, and `git` runs them.** Its hooks (`pre-commit`, `commit-msg`, `pre-push`...) run when **you** commit or push, exactly as in a terminal. Its configuration can name more: `core.fsmonitor`, `core.sshCommand`, `core.hooksPath`, `credential.helper`, `diff.external`, textconv programs, aliases, and `filter.<name>.clean`, which `git status` itself runs for the files that use it (Git LFS is one). So **before Emetos runs any `git` in a repository, it asks** "Do you trust the authors of the Git repository in <path>?", lists the programs that its local configuration names, and offers **Trust Repository**, **Trust All Repositories in <parent folder>** and **Don't Trust** (Escape). Until you trust it no `git` is started there. **Do not trust a repository you did not make unless you would build and run its code.** The answer is kept by the real path of the repository in `git-trust.json` (see [Where things are](#where-things-are)); Don't Trust lasts for the session. **Git: Manage Trusted Repositories...** removes an answer and stops its `git` processes. A repository that Emetos makes (Clone, Initialize Repository) is trusted at once.
- **After you trust it, Emetos's own background commands are hardened**: the status refresh, blame, the Graph, the Timeline and the quick diff run with `core.fsmonitor=false`, no pager, no external diff or textconv, no optional locks and no hooks, so a refresh does not start a program the configuration names. The one thing this cannot avoid is the **clean filters of `git status`**: switching them off would break Git LFS and the like. That is why the question exists.
- **Emetos stores no credential.** A username, password or passphrase that you type goes from the box to **the helper**, to `git`, and nowhere else; it is not kept, and it is never written to a log, a notification, a state file or the Git output. The helper is Emetos's own program: `git` is given three tiny scripts (`GIT_ASKPASS`, `SSH_ASKPASS` and `GIT_EDITOR`) that run `emetos --git-askpass` (or `--git-ssh-askpass`, `--git-editor`), which talks to the window over a Unix socket in a folder that only you can open (`$XDG_RUNTIME_DIR/emetos/git-<pid>/`, mode 0700, the socket 0600), with a random token that exists in memory and in the environment of git's commands. The scripts hold no secret, and a program of another user cannot connect. **Remembering** credentials is the job of git's own `credential.helper` (libsecret, `cache`, `store`), which git asks first, and of your SSH agent; the URL of a remote is shown in the output with its `user:password@` replaced by `***`.
- **The `safe.directory` rule is respected.** When `git` refuses a repository because another user owns it, Emetos says so and never overrides it; **Git: Manage Unsafe Repositories** can run `git config --global --add safe.directory <path>` after a confirmation that names the path and its owner. Beyond what a command you ran does (your `user.name` and e-mail after a question, that line, the upstream a push sets), Emetos writes nothing to your git configuration.
- **A client of Remote Work who uses Git uses yours**: your credential helpers, your keys and your agent ([Remote Work](#remote-work)).

### What is not built

**GitHub in every form** (sign-in, accounts, Publish to GitHub as a service, the clone list, pull request commands, Copilot features); VS Code's `vscode.git` **API for other extensions** (Emetos extensions talk through events, and the Git extension publishes `emetos.gitscm.repositoryChanged`); `scm.repositories.explorer` (the tree of branches, tags, stashes and worktrees, off by default in VS Code); `scm.workingSets.*`; the tasks VS Code runs after it creates a worktree; repositories of a multi-root workspace; notebook diffs and the accessible diff viewer; random branch names (`git.branchRandomName.*`); the Windows and macOS commands; "Local History" and "Pin Workspace Timeline" in the Timeline; `git reset --hard` (VS Code has none either).

### Where it is

The extension is `extensions/gitscm` (`git/` runs the program, `helper/` is the password helper, `gittest/` makes isolated repositories for the tests); the view is `internal/gui/views/scm`, the Graph `internal/gui/views/scmgraph`, the Timeline `internal/gui/views/timeline`, the peek `internal/gui/quickdiffpeek`, the multi-file diff `internal/gui/multidiff`, and the model they share `internal/core/scm`. The **Git** channel of the Output panel (**Git: Show Git Output**) logs each command, as `> git status -z -uall [34ms]`, and its errors. The tests use the real `git` in temporary repositories with their own `HOME`, configuration and author, and never touch yours.

## Containers and databases

Six extensions ([SPEC §10.20](docs/SPEC.md#1020-containers-docker-and-podman-extensionscontainers-extensionspodman) to [§10.24](docs/SPEC.md#1024-what-the-database-and-container-extensions-share-and-the-clients-of-a-hosted-workspace)) for the things a project runs next to its code. Each talks only to what **you** configured: a container engine, or the databases of your connections.

### Docker and Podman

**Containers** (the Docker extension) and **Podman** have the same view in the activity bar, in the spirit of VS Code's Container Tools: the containers grouped by Compose project (and by pod, for Podman), images, networks, volumes and contexts, with their state, ports and age. A container's menu starts, stops, restarts, pauses and kills it, shows its logs in the panel (following, with stderr in red and a filter), inspects it, runs a command in it or opens a shell in Emetos's Terminal, opens a published port in the browser, and **Connect to Database** (below). Images are pulled, tagged, pushed and removed; Build Image and Compose (Up, Down, Restart, Logs) run through your `docker` or `podman` program, with their output in the panel and the errors of a Dockerfile in Problems. Removing and pruning always ask first, with the count and the size, and Cancel changes nothing. The engine is found from the setting (`containers.host`, `podman.host`), `DOCKER_HOST` or `CONTAINER_HOST`, your Docker context, and the default sockets; no engine means the view says so.

![The Containers view over a Docker engine: Compose projects and their containers, and the logs of a container in the panel](docs/screenshots/docker.png)

### Databases and connections

The **Databases** view (the cylinder in the activity bar) lists your **connections** of every engine, with a state and a color you choose, and opens the databases, tables or keys below each one when you open it. *New Connection...* has the fields of the engine (host and port, user, database, or a file for SQLite, or a URL; TLS; **read-only**). Connections are kept in `db-connections.json` for every workspace or for one folder ([Where things are](#where-things-are)).

**A password is never written to a file of Emetos.** You choose between asking it each time (kept in memory until you disconnect), the **desktop keyring** (the Secret Service, labelled "Emetos: <connection>"), or an environment variable named in the connection (`${env:NAME}`). A password pasted inside a URL moves to the keyring or to memory.

### SQL

The **SQL** extension connects to **PostgreSQL, MySQL, MariaDB, SQLite and SQL Server** and shows their schema in the Databases view: databases, schemas, tables, views, functions and procedures, and below a table its columns (type, NOT NULL, keys), indexes and foreign keys. The menu of a table opens a statement in a new editor (Select Top 1000, SELECT, INSERT, UPDATE, DELETE, CREATE). A `.sql` editor is bound to a connection and a database (the status bar item): Ctrl+Enter runs the statement at the cursor or the selection, Ctrl+Shift+Enter runs them all, and Cancel stops a running one. There are Begin, Commit and Rollback (the status item is tinted while a transaction is open), a **read-only** connection refuses writes, and destructive statements are confirmed. The results are in a panel, a grid for every result set (only the rows in view are built, so a hundred thousand rows are fine; sort, copy as TSV, export to CSV, TSV, JSON or SQL `INSERT`s), with a Messages tab (rows affected, times, and errors with their place marked in the editor); the editor completes names from the live catalog.

![A SQL editor on a SQLite database with the grid of its result in the panel](docs/screenshots/sql.png)

### MongoDB

The **MongoDB** extension ([SPEC §10.22](docs/SPEC.md#1022-mongodb-extensionsmongodb)) adds the engine MongoDB to the Databases view: *New Connection...* asks for a host and a port or a connection string (`mongodb://`, `mongodb+srv://`), the user, TLS and, as you need them, the auth source and mechanism (SCRAM or an X.509 certificate), the replica set, a direct connection and the read preference. Below a connection the view lists the databases (with their size), the collections (with how many documents they have, from the server's metadata), the **indexes** of a collection and its **schema**: the fields and their types, found in a sample of up to `mongodb.schemaSampleSize` documents (100).

A **playground** is a `.mongodb` file with statements in the syntax of `mongosh` (`use shop`, `show collections`, `db.orders.find({status: "open"}).sort({total: -1}).limit(20)`, `db.runCommand({ping: 1})`); Emetos reads them itself, without JavaScript, and says where a statement it does not understand is. **New Playground** in the menu of a connection, a database or a collection opens one with the first lines written, and **Open Collection** shows the first page of a collection. Ctrl+Enter runs the selection or the statement at the cursor and Ctrl+Shift+Enter runs them all, on the connection and the database in the status bar item. The results are in a panel as a **Tree**, **JSON** or a **Table**, with a Messages tab (what each statement returned or changed, errors with their code and place); cursors are read in pages (`mongodb.pageSize`). A document opens as Extended JSON in an editor: Ctrl+S shows the fields that changed and, after you answer, replaces the document; Delete Document asks first. **Create Collection**, **Create Index**, **Drop Collection**, **Drop Index** and **Drop Database** only open a playground with the statement written for you to read and run: the menu never changes the server. A read-only connection refuses writes before anything is sent, and `mongodb.confirmDestructive` asks before a drop, `deleteMany({})` and `updateMany({}, ...)`.

![A MongoDB playground on the orders collection, with its documents in the results panel](docs/screenshots/mongodb.png)

### Redis

The **Redis** extension ([SPEC §10.23](docs/SPEC.md#1023-redis-extensionsredis)) works with Redis, Valkey and compatible servers. A connection is a URL or a host with an ACL user, TLS, a database number, a key separator, **Sentinel** or **Cluster** mode, and read-only. A database lists its keys as a tree split at the separator (`:`), loaded with `SCAN` in pages (`redis.scanCount`, never `KEYS`) with a filter by text or pattern. A key opens in an **editor**: a string (text, JSON or hex), a hash, list, set, sorted set or stream (grids that page and let you add, edit and remove), or a ReJSON value, with Rename, Set TTL, Persist and Delete.

![The key editor on a Redis hash, beside the Databases view](docs/screenshots/redis.png)

The **Redis** tab of the panel has a **console** (a command line with history, `redis-cli` quoting, completion of command names from the server, replies formatted by type, and a cancel for a blocking command), **Pub/Sub** (Subscribe, PSubscribe and Publish) and **Monitor** (with a warning first), each bounded, with pause and clear, and the **server information** (`INFO`, `SLOWLOG`, `CLIENT LIST`, `MEMORY STATS`). `.redis` files run their lines with Ctrl+Enter. Dangerous commands ask first, and a read-only connection refuses writes.

### Connect to Database

On a container of a database image the extensions know (`postgres`, `mysql`, `mariadb`, SQL Server, `mongo`, `redis` and `valkey`), the menu has **Connect to Database**: the connection form opens already filled with the host, the published port and the user and database of the image's environment, never a password.

### Security notes of containers and databases

- **Access to the Docker socket is power over the computer.** Whoever can use it can read and change any file as root. Emetos only uses the engine you point it at, shows a note about this in the view, and asks, with the count and the size, before it removes or prunes anything. **Podman's rootless socket gives only your own power**, but a rootful socket is the same as Docker's.
- **Database passwords are in the desktop keyring or in memory, never in a file of Emetos**, and no connection file with a password in it is accepted. A password never appears in an error or a log, and neither do query text or values: the logs and events carry none. Without a keyring the password is asked, and a warning says so when you asked to save it.
- **A database is power over its data.** Use **read-only** connections (and a database user that can only read) for what you only look at, and read what a statement does before you run it. Emetos asks before the destructive ones, but it does not know what your triggers and procedures do. Do not connect to a server over an untrusted network without TLS.
- **Clients of a hosted workspace cannot run these commands.** The commands of the six extensions reach outside the workspace folder, so they are on the list of commands refused to a client ([SPEC §10.24](docs/SPEC.md#1024-what-the-database-and-container-extensions-share-and-the-clients-of-a-hosted-workspace)), with the rest of the commands that leave the workspace.

## Run and Debug

**Run and Debug** ([SPEC §10.31](docs/SPEC.md#1031-run-and-debug-internalcoredap-internalcoredebug-internalguiviewsdebug-the-language-extensions)) is VS Code's debug workbench over the **Debug Adapter Protocol**: its view, toolbar, breakpoints, Debug Console, commands, keys and settings (`debug.*`, `launch`). Emetos speaks DAP and **runs the debugger you installed**, the way it runs a language server; it installs none (see the debuggers below). [The coverage table of area 6](docs/IMPLEMENTED.md#coverage-of-area-6-run-and-debug-spec-3-1031) lists its 22 rows with their tests.

![Run and Debug: a Go program stopped at a breakpoint under Delve, with the Variables, Call Stack and Breakpoints views, the values after the line, the toolbar, the orange status bar and the Debug Console](docs/screenshots/debug.png)

### How to use it

1. **Start.** Press **F5** (Ctrl+F5 runs without debugging), or open **Run and Debug** (Ctrl+Shift+D, the icon of the activity bar, the Run menu) and press the green button. Without a `launch.json`, F5 starts the file of the editor with the debugger of its language; with configurations, the **dropdown** of the view picks one, the status bar shows it, and **Add Configuration...** inserts the snippets of a debugger.
2. **`launch.json`.** The configurations are in `<folder>/.emetos/launch.json` (VS Code's format, JSONC: `configurations`, `compounds`, `inputs`, and its variables such as `${workspaceFolder}`, `${file}`, `${env:NAME}` and `${input:id}`) and in the `launch` key of your settings or the folder's. `.vscode/launch.json` is not read. The gear opens the file; its mistakes show in Problems, and completion offers the attributes of each debugger.
3. **Breakpoints.** **F9** sets one at the cursor and a click in the margin does too; Shift+F9 sets an inline breakpoint at a column; a right click, or the breakpoint widget, gives conditions, hit counts and log messages (logpoints). The Breakpoints view lists them, with the exception breakpoints and function breakpoints. They follow your edits, and are kept with the workspace.
4. **While it runs.** The toolbar floats over the editor area (`debug.toolBarLocation`): Continue F5, Pause F6, Step Over F10, Step Into F11, Step Out Shift+F11, Restart Ctrl+Shift+F5, Stop Shift+F5. The stopped line, **Variables**, **Watch**, **Call Stack** and **Loaded Scripts** show where the program is, the values appear after the lines (`debug.inlineValues`), and the pointer on an expression shows its value. **Run to Cursor** and **Jump to Cursor** are in the editor's menu.
5. **The Debug Console** (Ctrl+Shift+Y, a tab of the panel) shows the program's output with its colors and what the debugger says, has a filter, and evaluates what you type in the frame you picked (history with Up and Down, completions from the debugger).
6. **Tasks before and after.** `preLaunchTask` and `postDebugTask` name a task of the **Tasks** extension: an entry of `emetos.tasks.commands`, a command line or `{"label": "build", "command": "go build ./..."}` (there is no `tasks.json`). A failing `preLaunchTask` follows `debug.onTaskErrors` (`prompt` asks: Debug Anyway, Show Errors or Abort).
7. **Remote Work.** A client who uses your workspace can debug in it: its breakpoints, F5, steps and Debug Console work on the host's session, and both screens show the same session. The program runs, and the debugger starts, on the host ([Remote Work](#remote-work)).

### The debuggers

| Language | Debug type | Debugger | How to get it | Settings |
|---|---|---|---|---|
| Go | `go` | [Delve](https://github.com/go-delve/delve) (`dlv dap`): programs, tests, packages and attach | **Go: Install/Update Tools** in the Command Palette (`go install`; a missing `dlv` is told with an **Install** button). The lenses "debug test" and "debug benchmark" above test functions and **Go: Debug Test At Cursor** use it | `go.alternateTools` (`dlv`), `go.toolsEnvVars` |
| Python | `debugpy` | [debugpy](https://github.com/microsoft/debugpy), run by the interpreter of the program: files, modules, a remote attach, an attach to a process, Django, FastAPI, Flask and Pyramid | `python -m pip install debugpy` in the interpreter you picked ("Python: Select Interpreter"); a missing debugpy is told with an **Install** button that types the pip command in a terminal | `python.defaultInterpreterPath` |
| C, C++ and Rust | `gdb`, `lldb-dap` | **gdb 14 or newer** (`gdb -i dap`) or **lldb-dap** (LLVM), from your system (`sudo dnf install gdb` or `lldb`, `sudo apt install gdb` or `lldb`, ...); an older gdb is told as too old, and the notification can copy the install command. Rust: the targets of `cargo metadata` are in the dropdown, and a configuration with `"cargo"` runs `cargo build` first and debugs the executable it names | `gdb.path`, `gdb.arguments`, `gdb.environment`, `lldb-dap.executable-path`, `lldb-dap.arguments`, `lldb-dap.environment` |
| JavaScript and TypeScript | `node` (`pwa-node`) | [js-debug](https://github.com/microsoft/vscode-js-debug), VS Code's own debugger, run by **your `node`**: scripts, the scripts of `package.json`, attach to a process, child processes and workers; TypeScript through source maps (`outFiles` from the `tsconfig.json`) | **JavaScript Debugger: Download** in the Command Palette fetches the pinned release (v1.140.0) **on your click**, checks its size and SHA-256, and unpacks it into Emetos's data folder; or point `debug.javascript.adapterPath` at your own js-debug (its `dapDebugServer.js`, or the folder that holds `src/dapDebugServer.js`) | `debug.javascript.adapterPath` |

Every other setting of VS Code's debug section is there with its default (the settings editor lists them under Debug): `debug.saveBeforeStart`, `debug.openDebug`, `debug.focusEditorOnBreak`, `debug.focusWindowOnBreak`, `debug.confirmOnExit`, `debug.toolBarLocation`, `debug.console.*` and more.

**Tested** against Delve v1.27.2, debugpy 1.8.22, gdb 18.1, lldb-dap (the system's) and js-debug v1.140.0 with node v22, each on a small program in `testdata/`, headless, with a temporary home folder and no network, plus a scripted fake adapter for everything else; the whole path (F9, F5, hover, watch, F10, the Debug Console, Shift+F5) runs with real input under Delve. The speed of stepping, of a Variables view with 10,000 children and of a console with 100,000 lines is in [`docs/PERFORMANCE.md`](docs/PERFORMANCE.md). The pass on a real GPU and a session by hand are in `docs/MANUAL-CHECKS.md` for the author.

### What is not built in Run and Debug

Chrome and Edge debugging, the `node-terminal` type, auto attach and profiling of js-debug, and the "JavaScript Debug Terminal"; Python's Flask and FastAPI detection, `serverReadyAction` and the pickers `debugpy.pickLocalProcess` and `debugpy.pickArgs`; the other `lldb-dap.*` settings and the lens of rust-analyzer that debugs; `console: integratedTerminal` for Go (`asRoot` and `logDest` too); data and instruction breakpoints, memory and disassembly views, and sources the adapter serves; VS Code's `tasks.json` (a task is an entry of `emetos.tasks.commands`), the dialog for a task that is not found (**Configure Task**) and the notice for a `preLaunchTask` that takes more than 10 seconds; compounds start in order, not in parallel; and a `.vscode/launch.json`.

## Testing

**Testing** ([SPEC §10.32](docs/SPEC.md#1032-testing-internalcoretesting-internalguiviewstesting-the-language-extensions)) is VS Code's Testing workbench over an API that the language extensions use to describe their tests and report their runs: the **Test Explorer**, the glyphs in the editor's margin, failures at their lines and in a peek, **Test Results** and **Test Coverage**, with the commands, keys and settings of VS Code (`testing.*`). Emetos **runs the test tool you installed**, the way it runs a language server; it installs none (see the providers below). [The coverage table of the testing UI](docs/IMPLEMENTED.md#coverage-of-area-10-the-testing-ui-spec-3-1032) lists its 26 rows with their tests.

![Testing: the Test Explorer after a run of a Go package with coverage, the Test Coverage view, cart.go with its covered and uncovered lines, and the failure of a test peeked with its expected and actual values](docs/screenshots/testing.png)

### How to use it

1. **See the tests.** Open **Testing** (the flask icon of the activity bar, or "Testing: Focus on Test Explorer View" in the Command Palette). The tree lists what the providers found; the filter box takes text and VS Code's terms (`@failed`, `@executed`, `@doc`, `@hidden`, `@<provider>:<tag>`), and the "..." menu sorts by status, location or duration and shows the tests as a tree or a list. A summary line says how the last run went ("5/6 tests passed (83.3%), 222 ms"), and the icon shows how many tests failed.
2. **Run.** **Run All Tests** is Ctrl+; A, **Run Test at Cursor** Ctrl+; C, **Run Tests in Current File** Ctrl+; F, **Rerun Failed Tests** Ctrl+; E and **Rerun Last Run** Ctrl+; L; the same with Ctrl added for Debug (Ctrl+; Ctrl+A) and with Ctrl+Shift for Run with Coverage. Each test has Run, Debug and Run with Coverage buttons in the Test Explorer, and a **glyph in the margin** of the editor: a click runs the test (`testing.defaultGutterClickAction`), a right click has the menu. **Cancel Test Run** stops a run. Tests that did not change can run again by themselves: **Start Continuous Run**.
3. **Read a failure.** A failed test shows its message after its line and its glyph turns red. **Peek Output** (Ctrl+; Ctrl+M) opens the peek: the message, the expected and actual values side by side, the stack and the output, with Alt+F8 and Shift+Alt+F8 for the next and previous failure and Escape to close it. **Test Results** (a tab of the panel) keeps the output of the runs, and the last 16 runs are kept with the workspace.
4. **Coverage.** **Run All Tests with Coverage** opens the **Test Coverage** view (files and folders with their statement, branch and function percentages and bars) and shows the covered and uncovered lines in the editor, with their counts; the percentages show in the Explorer too (`testing.showCoverageInExplorer`).
5. **Debug.** The Debug button starts the debugger of the language on the test ([Run and Debug](#run-and-debug)); it makes no result of its own.
6. **Remote Work.** A client who uses your workspace sees the Testing view and the glyphs of the host, and runs and debugs tests as at the host's keyboard: the run is the host's and every screen shows its result ([Remote Work](#remote-work)).

### The providers

| Language | Controller | Test tool | What it needs | Settings |
|---|---|---|---|---|
| Go | `go` | `go test -json`: packages, files, `Test`, `Benchmark`, `Example` and `Fuzz` functions, and the subtests that a literal in `t.Run` names (the others appear when a run reports them) | `go` on `PATH`. Debug: Delve ([Run and Debug](#the-debuggers)) | `go.testFlags`, `go.testTimeout`, `go.testEnvVars`, `go.testEnvFile`, `go.toolsEnvVars` |
| Python | `python` | **pytest** or **unittest**, through a small helper that Emetos writes to its cache folder and runs with your interpreter (a pytest plugin, a unittest runner): folders, files, classes, functions and the cases of a parametrized test | the interpreter of "Python: Select Interpreter"; pytest for pytest (`python -m pip install pytest`); **coverage.py** for Coverage (`python -m pip install coverage`: without it you are told and the tests run plain); `debugpy` for Debug | `python.testing.pytestEnabled`, `unittestEnabled`, `pytestArgs`, `unittestArgs`, `cwd`, `autoTestDiscoverOnSaveEnabled`, `autoTestDiscoverOnSavePattern` |
| Rust | `cargo` | `cargo test`: the packages of the workspace, their targets (lib, bin, test, example, doc tests), modules and tests, read from libtest's output | `cargo` on `PATH` (discovery builds the tests). Debug: gdb 14 or newer, or lldb-dap | `rust-analyzer.runnables.extraArgs` |
| JavaScript and TypeScript | `node-test`, `jest`, `vitest` | **Node's test runner** (`node --test`), **Jest** and **Vitest**, each when the project uses it (`package.json` or its config files): test files, `describe` suites and `it`/`test` calls with literal names | `node`; Jest and Vitest from the project's `node_modules`; `@vitest/coverage-v8` or another `@vitest/coverage-*` package for Vitest's Coverage. Debug: js-debug ([Run and Debug](#the-debuggers)) | `typescript.nodePath` (the `node` to use) |
| C and C++ | `ctest` | **CTest**, for a CMake project that is already configured and built: the tests of the build folder, grouped by a delimiter you choose | `cmake`'s `ctest` on `PATH` and a build folder with a `CTestTestfile.cmake`. Debug: gdb 14 or newer, or lldb-dap | `emetos.cpp.ctest.buildDirectory` (`build`), `emetos.cpp.ctest.testSuiteDelimiter` |

**Run, Debug and Coverage by provider.** Every provider has **Run**. **Debug** is there for Go (Delve, mode `test`), Python (debugpy, on `pytest` or `unittest`), Rust (gdb or lldb-dap on the test binary), JavaScript and TypeScript (js-debug on Node's runner, Jest or Vitest) and C and C++ (gdb or lldb-dap on the test's command). **Coverage** is there for Go (`-coverprofile`: statements), Python (coverage.py: lines, branches), JavaScript and TypeScript (Istanbul's `coverage-final.json` for Jest and Vitest, `--experimental-test-coverage` for Node's runner) and not for Rust or C and C++. **Continuous runs** work for Go, Python, Rust and JavaScript and TypeScript, and not for C and C++ (ctest does not build, so a changed source would run the old program).

**Tested** with fake runners everywhere (programs that print what the real tool printed, recorded from it), with the real `go` in the normal tests, and, one tool at a time and skipped when it is missing, against pytest 9.2.0 with coverage.py 7.17.0, the system's cargo, node v22 with Jest 30.5.2 and Vitest 5.0.5, and the system's CMake 4.3.0 and ctest; the whole path of the Go provider (the Test Explorer, a run, the glyph, the peek, a rerun of the failed tests, coverage, Escape) runs with real input and the real `go`. The speed of 5,000 Go tests (their discovery, a full run and the Explorer's scroll) is in [`docs/PERFORMANCE.md`](docs/PERFORMANCE.md#testing-step-2610). The pass on a real GPU is in `docs/MANUAL-CHECKS.md` for the author. A test runs your program with your permissions, so run only tests you would run.

### What is not built in Testing

The per-test coverage filter and the minimap marks of coverage; test tags beyond the filter; the run profile submenus of VS Code's run buttons ("Execute Using Profile", "Start Continuous Run Using Profile"); a setting for the number of results kept (16); the "no tests for this document" widget and the mark of a failure in the overview ruler; test-message menus and attachments; test generation and "Go to Related Code". **Go:** branch coverage, and tests in files that build constraints exclude, until a run reports them. **Python:** `pytestPath`, a command to configure the tests, pytest-xdist and nose, and the values of `assertEqual`. **Rust:** coverage, benchmarks, tests that a macro makes or a `#[path]` module holds, and custom harnesses. **JavaScript and TypeScript:** Jest's `projects` and Vitest's workspaces, the `t.test()` subtests of Node's runner before a run, live results of Jest and Vitest, and a setting for the tool's path. **C and C++:** coverage, continuous runs, building before a run, `-C <config>` for multi-configuration generators, CMake presets and `ctest -j`. For every provider, a debug session makes no test result.

## HTTP Client

The **HTTP Client** ([SPEC §10.33](docs/SPEC.md#1033-http-client-extensionshttpclient)) builds, sends and keeps HTTP requests the way Postman does: collections and folders in a side bar, a form for each request, a response pane, environments with variables, a history and a collection runner. It is an Emetos extension (`emetos.httpclient`) written with Go's standard library only. Its collections are **readable files in your project**, so a team shares them through git, and it has **no scripts**: values are taken from responses and tested by declarative rules. [The coverage table of area 12](docs/IMPLEMENTED.md#coverage-of-area-12-the-http-client-spec-3-1033) lists its 20 rows with their tests.

![The HTTP Client: the Collections, Environments and History views, a POST request with its JSON body, and the response pane with the status, time and size and the formatted JSON](docs/screenshots/httpclient.png)

### How to use it

1. **Open it.** Click the globe icon of the activity bar (or "HTTP Client: Focus on Collections View" in the Command Palette). **New Collection**, **New Folder** and **New Request** are in the view's menus; a request opens in a tab of its own, and a new one is untitled until you save it into a collection. The context menu has Rename (F2), Duplicate, Delete (to the trash, with the body files), Run Collection and Run Folder; drag and drop moves and reorders; the filter box finds a request.
2. **Send.** The request tab has the method (a dropdown, or type your own), the URL, and the tabs **Params**, **Headers**, **Body** (none, JSON, XML, text, HTML, JavaScript, GraphQL, form, multipart with files, binary), **Auth** (inherit, none, basic, bearer, API key, digest and OAuth 2.0), **Settings** (timeout, redirects, TLS verification), **Extract** and **Checks**. **Send** is Ctrl+Enter, Cancel stops it, Ctrl+S saves. Params and the URL stay in step; `name: value` lines can be edited in bulk.
3. **Read the response.** Below the request (or beside it: `httpClient.responseLayout`) are the status, the time and the size (hover for DNS, connect, TLS, first byte and download), and the tabs **Body** (Pretty, the body formatted by its `Content-Type` (JSON, XML, HTML), Raw, and a Preview of images), **Headers**, **Cookies**, **Timeline** (the redirects, each request and response with its headers, the TLS version and certificates) and **Checks**. Save Response to File, Copy Body and Open in New Editor Tab are in the pane. A body over 50 MiB (`httpClient.maxResponseMiB`) is streamed to a cache file and read in pages. HTML is shown as text; there is no web view.
4. **History.** The last 200 requests you sent (`httpClient.historyLimit`) are grouped by day, with the secrets and `Authorization` values masked; **Open** makes a new request of one, **Save to Collection...** keeps it.
5. **Remote Work.** The commands of the HTTP Client are refused to a client of a hosted workspace: a request leaves from the host's network, as a database connection does (see below).

### The format

Everything is under `<folder>/.emetos/http/`, in JSON with comments (`.jsonc`), version 1 (`"emetosHttp": 1`). Emetos reads only this folder, not Postman files or `.http` files.

```
.emetos/http/
  environments/dev.jsonc            { "emetosHttp": 1, "name": "Dev", "variables": [ { "name": "baseUrl", "value": "https://api.example.test" }, { "name": "apiKey", "secret": true } ] }
  collections/shop-api/
    collection.jsonc                name, description, order, variables, auth, headers of the collection
    users/folder.jsonc              the same keys for a folder
    users/create-user.request.jsonc method, url, params, headers, auth, body, settings, extract, checks
    users/create-user.body.json     the body of that request, a file of its own
```

A body of text is a file of its own (`.body.json`, `.xml`, `.txt`, `.html`, `.js`, `.graphql`), so it has its own highlighting and clean diffs in a review. File names are the names made safe, and the `order` list keeps the order of the tree. Files are read with comments and trailing commas, and Emetos edits them so that **your comments and the keys it does not know are kept**. A file with a newer version number opens read-only with a notice, and a file it cannot read is shown with its error. A path outside the folder (a body file, a client certificate) is refused. Changes made on disk (an editor, `git pull`) show at once. Opening a `.request.jsonc` from the Explorer opens the request tab, with **Open as Text** in its title bar.

### Environments and variables

`{{name}}` works in the URL, params, headers, authorization fields, form fields and body files (a body file is changed only in what is sent). The order is Postman's: values extracted by the request, then the active environment, then the folders (the innermost first), then the collection, then the globals; a name that is not found is left as it is, underlined, with a warning. Dynamic variables: `{{$guid}}`, `{{$timestamp}}`, `{{$isoTimestamp}}`, `{{$randomInt}}`, `{{$randomUUID}}` and `{{$processEnv NAME}}`, which reads only the names you list in `httpClient.allowedProcessEnv` (empty by default, so a collection cannot read your environment unasked).

- **Environments** are listed in the Environments view; the active one is checked and shows in the status bar ("HTTP: Dev", with a picker). The environment editor is a table of name, initial value, current value, secret and enabled.
- **Initial value and current value.** The value in the file is the initial one and is shared through git. The **current value** is yours only: extraction rules and the table write it to Emetos's state, never to a file of the project, so running requests changes no file. **Reset** puts the initial value back and **Persist** writes the current value into the file (not for secrets).
- **Secrets.** A variable marked secret has **no value in any file**: the file says `"secret": true`, and the value is in the **desktop keyring** (Secret Service), asked for when it is missing. The passphrase of a client key and OAuth tokens are in the keyring too. History, cookies (one jar per environment, with a Cookies tab to see and clear them) and current values are in Emetos's state.
- **Globals** are variables of your user, shared by all projects, and are not in a project.

### Extraction and checks

Without scripts, a request has two lists of rules, each with a form in its tab:
- **Extract** rules take a value after the response (from the body, a header, the status or a cookie) into a variable of the environment, the collection or the globals. A body is read with a **JSONPath subset**: `$`, `.name`, `['name']`, `[n]`, `[-n]`, `[*]` and `.*` (the first match is taken) and `..name` (the first match at any depth); filters, slices, unions and script expressions are refused with a clear message. A Go regular expression can take a part (its first group). A rule that finds nothing leaves the variable as it was, and the line under it says why. The next request uses the value.
- **Checks** compare a part of the response: the status, the time, a header, the body or a JSON path, with `equals`, `notEquals`, `contains`, `matches` (a regular expression), `lessThan`, `greaterThan`, `exists`, `notExists` and `isType`. The **Checks** tab of the pane lists each one as passed or failed with the actual value (the tab says `Checks (3/5)`), and the runner counts them.

### The collection runner

**Run Collection...** or **Run Folder...** (or the command `emetos.httpclient.runCollection`) opens a tab with the choices: the environment, the number of iterations, a delay between requests, stop on the first failure, the requests to run and, if you want, a **data file**: a CSV with a header row, or a JSON array of objects. Each row is one iteration and its columns are variables that win over every other. The runner sends the requests one at a time, with extraction carrying values from one to the next, and shows a table of each request of each iteration (result, status, time, size and checks, expandable to the URL, the notes, the checks and the extractions), an iteration picker and a summary. **Cancel** stops it, and **Save Results** writes a JSON report where you choose (it holds no data value). Results are not kept when the tab closes, and runs are not recorded in the History.

### OAuth 2.0

The Auth tab sets up OAuth 2.0 with the **client credentials** grant or the **authorization code** grant with PKCE (S256). **Get New Access Token** opens your browser (through Emetos's "open link", on your click) and receives the code on a loopback redirect (`http://127.0.0.1:<random port>/callback`, RFC 8252) that lives only while it waits and closes after a success, a failure, a Cancel or five minutes. The token is kept in the **keyring**, per collection and grant, masked in the form, sent the way `addTo` says and refreshed with its refresh token when it has expired; **Clear Token** deletes it. A send from the request tab or the runner never opens a browser by itself: when no token can be had, it tells you to get a new one.

### Settings

`httpClient.timeoutMs` (30000), `httpClient.verifyTls`, `httpClient.proxy` (the proxy of the environment, `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`, is used otherwise), `httpClient.clientCertificates` (certificate and key files by host pattern), `httpClient.historyLimit` (200), `httpClient.responseLayout` (`vertical` or `horizontal`), `httpClient.allowedProcessEnv` and `httpClient.maxResponseMiB` (50). The settings that decide where requests go (the proxy and the certificates) are read from your user settings only. A redirect is followed up to 10 times, a request turns TLS verification off with a warning badge, and `User-Agent` is `Emetos/<version>` unless you set one.

### Privacy and Remote Work

Emetos sends **only the requests you send** and nothing else: nothing is synced to a server, and the project gets only what you save. A client of a hosted workspace cannot run the commands of the HTTP Client: they are on the list of commands refused with the class `outside` ([SPEC §10.24](docs/SPEC.md#1024-what-the-database-and-container-extensions-share-and-the-clients-of-a-hosted-workspace)), they are not in its Command Palette, and its side bar views say so instead of showing the host's collections, environments and history. The collections are still files of the workspace that a client can read and edit as files.

**Tested** against `httptest` servers and a fake authorization server on `127.0.0.1` (no test reaches the network), with the keyring tested through a fake and, by hand, not yet against a real one (`docs/MANUAL-CHECKS.md` lists it for the author, with an OAuth login at a provider of your choice and the pass on a real GPU). The whole path (make a collection, send, extract a token, use it in a second request, run the collection) runs with real input. A collection of 1,000 requests, a 50 MiB JSON response and a run of 500 requests are measured in [`docs/PERFORMANCE.md`](docs/PERFORMANCE.md).

### What is not built in the HTTP Client

- **Scripts**, on purpose ([D44](docs/SPEC.md#4-decisions-log)): there are no pre-request or test scripts, no `pm.*` API and no JavaScript engine. Extraction and checks are rules, and the runner and data files cover most loops.
- **Import and export of other formats**, on purpose (D44): no Postman, Insomnia or OpenAPI files, no `.http` files and no cURL import, and no code generation. The format is Emetos's own.
- **Protocols and bodies:** WebSocket, gRPC, HTTP/3, Server-Sent Events as a stream (the body is read to its end), GraphQL schema introspection and completion, and rendering of HTML responses. **Brotli** is not decoded: the response is shown as it came, with its `Content-Encoding` header (gzip and deflate are decoded).
- **Authorization:** the **password** and **implicit** OAuth grants (OAuth 2.1 drops them), the device flow, a token endpoint with a client certificate, `private_key_jwt`, a custom redirect URI, a refresh in the background before a send you did not start, and the AWS, Hawk, NTLM and Akamai authorizations. A client key's passphrase works for the legacy PEM encryption; **PKCS#8-encrypted keys** are not read.
- **Around the client:** mock servers, monitors and sharing through a server. The runner's table does not scroll to the newest row by itself while a run goes on.

## Remote Work

An extension that lets one Emetos be used from another computer. **It is new and experimental**, and **off until you turn it on**. It logs in over **SSH with public keys only** (no passwords): the computer that is used lists the keys that may log in.

**Remote Work** ([SPEC §10.18](docs/SPEC.md#1018-remote-work-remote-control-of-a-workspace-extensionsremotework)) shows **one workspace of computer A as a window tab on computer B**, and B works in it with its keyboard and mouse. A runs a second view of the workspace, so both see the same documents. On A, the page **Remote Work** (open it with **Remote Work Settings...** in the Sharing menu) turns it on for a workspace, sets the port (**47123**, TCP and UDP), the idle timeout (30 minutes) and the allowed keys. On B, the `+` menu has **Remote Work...**: host, port and a private key file or the SSH agent. The picture travels over QUIC (UDP) and falls back to SSH over TCP when UDP does not get through. Not built: sound, drag and drop between the computers, more than one workspace per session, and a sharp picture on a HiDPI screen (it is drawn at logical pixels).

**What B gets in its view** ([SPEC §10.18](docs/SPEC.md#1018-remote-work-remote-control-of-a-workspace-extensionsremotework), D50 and D51): the view shows what the extensions of the host give a window, so B has the **Source Control** view, the **Graph**, the **Timeline**, the file decorations, and the commands, menus and status items of the extensions, and it uses **Git as if it sat at A's keyboard**: commit, branches, fetch, pull, push, clone and the password boxes included. A question that a command of B raises (a password, a "trust this repository?", a choice) is shown **in B's view**, and the one that nobody asked for (an automatic fetch) stays in A's window. B also sees **the host's terminals** live in the panel of its view and can type in them, make new ones, split, rename and close them, as at A's keyboard; everyone who looks at a terminal sees the same screen, and its size follows the one who typed last. **This gives B a shell on A's computer**: see the first of [the security notes](#security-notes). The commands that leave the workspace stay refused to B: the window tabs, the folder, the profiles, your user settings, quitting, the commands of the container and database extensions, and the ones that start a program on A's screen.

![A Remote Work tab: the workspace of another computer, shown in a tab of this window (here both on one machine)](docs/screenshots/remote-work.png)

### How to host a workspace

The **Sharing** item at the left end of the status bar (the broadcast icon) says whether Remote Work hosts the workspace: the icon alone when it is not, **Hosting** (with the number of computers connected, "Hosting · 2") on a blue background when it is. Click it to host or to stop.

1. **Add the key.** Click the item, choose **Host with Remote Work**. With no key listed, the page opens at its guide: paste the public key of the computer that will connect (the contents of its `id_ed25519.pub`), choose the `.pub` file, or Scan QR Code.
2. **Turn it on.** Choose **Host with ...** again (or check **Host this workspace** on the page). The guide gets a check mark for each step, and the item says Hosting.
3. **Connect from the other computer.** The guide lists the addresses of this computer with the port, each with a Copy button, and the fingerprint of the host key to compare the first time. On the other computer, the `+` menu has **Remote Work...**.

The commands are also in the Command Palette: **Remote Work: Host This Workspace**, **Sharing: Show Hosting Menu**.

### How to stop

Click the Sharing item and choose **Stop Hosting with Remote Work**: the sessions of the workspace end at once, and the other computer reads "The host stopped sharing this workspace." The same is **Stop Hosting** on the banner of Remote Work, the **Stop Hosting** button beside the switch on the page, and the commands **Stop Hosting This Workspace**. **Disconnect <key>** in the same menu ends one computer's session and leaves hosting on. Nothing listens once nothing is hosted.

### Security notes

Read these before you turn the extension on ([SPEC §10.18](docs/SPEC.md#1018-remote-work-remote-control-of-a-workspace-extensionsremotework) has the whole design):
- **A listed key gives that person a shell on your computer, and your Git.** Since D50 and D51 the other computer can open and type in your terminals: it can run any program as **you**, read any file you can read (not only the workspace's) and use whatever your session has unlocked, such as your SSH agent. It can run every Git command with **your** `git`, your credential helpers, your SSH keys and your agent, so it can push as you (there is no refusal for Git: the user's choice, D38). It can also edit and delete files in the workspace and run tasks. List only keys you would trust with your account and your files, and stop hosting when you do not need it.
- **Compare the fingerprint the first time.** The first connection shows the host's key fingerprint; compare it with the one on the host's page before you trust it. A connection is only as safe as that comparison. A changed host key is refused with a warning.
- **Over the internet, use a VPN.** The login is as safe as SSH with key-only login, and a VPN is better still. Do not expose the ports to the internet without one. Failed logins are limited to 5 a minute for each address, but that is not a defense by itself.
- You see every login: a banner in the workspace (with Disconnect), a notification, and a log entry. Turning the feature off or removing a key ends the sessions it logged in, and a Remote Work session also ends after its idle timeout.
- A computer that is logged in sees the terminals you have open, with what they show, and the Source Control view with your changes. Close a terminal that shows something you do not want to show before you share the workspace.
- No settings file can turn it on or add a key: only its own page does. Emetos keeps no private key of yours: it remembers the path of your key file, reads the file when you connect, and asks a passphrase in a dialog without keeping it. The only private key it keeps is its own host key (`remote-work-host-key`, mode 0600, see [Where things are](#where-things-are)).

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
| `--install-launcher` | add Emetos to the application menu and launcher (a desktop entry and icon under `$XDG_DATA_HOME`) and exit |
| `--uninstall-launcher` | remove them and exit |
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
| Git: the repositories you trusted (by real path) and the folders you trusted all of | `$XDG_STATE_HOME/emetos/git-trust.json` (mode 0600) |
| Git: the socket and the three scripts of the password helper, one folder for each window tab, removed when Emetos ends | `$XDG_RUNTIME_DIR/emetos/git-<pid>/` (mode 0700; `$XDG_STATE_HOME/emetos/git-<pid>/` when there is no runtime folder) |
| Database connections (SQL, MongoDB, Redis), of every workspace or of one folder: name, engine, host, port, user, options, never a password | `$XDG_STATE_HOME/emetos/db-connections.json` (mode 0600) |
| Database passwords you chose to keep | the desktop keyring (Secret Service), labelled "Emetos: <connection>"; never a file |
| HTTP Client: collections, folders, requests and environments, which you share through git | `<folder>/.emetos/http/` |
| HTTP Client: the history, the cookies of each environment and the current values | Emetos's state (`$XDG_STATE_HOME/emetos/`), never the project |
| HTTP Client: secret variables, client-key passphrases and OAuth tokens | the desktop keyring (Secret Service); never a file |
| The socket of the Podman service Emetos starts when none answers | `$XDG_RUNTIME_DIR/emetos/` (Emetos stops that service when it ends) |

The files of the Event sharing, Remote Work, Git trust and database connection rows are in the state folder of Emetos and are the same for every profile; the folder has mode 0700. Only their own pages write them, and **Emetos refuses a file that other users can write** (a private key also one they can read): it does not use it and does not replace it. The host and key file of a Remote Work tab are part of the window state. Deleting `remote-work-host-key` makes a new identity, and every client must trust it again.

The files are JSON with comments, in VS Code's formats. "Preferences: Open User Settings (JSON)" and "Open Keyboard Shortcuts (JSON)" open them in Emetos.

## Privacy

Emetos sends nothing about you anywhere: **no telemetry, no crash reports and no update checks.** It uses the network only for what you ask of a feature, and these are the things that do:
- **Remote Work**: only while you have turned it on, and only with the computers whose keys you listed or whose hosts you saved.
- **Git**: only the remotes that `git` contacts for what you asked (fetch, pull, push, clone, publish), with your credential helpers and your SSH keys. **`git fetch` on a schedule (`git.autofetch`) is off until you answer**: after the first successful remote command Emetos asks once, "Would you like Emetos to periodically run "git fetch"?", and runs nothing on a schedule unless you say Yes (No and Ask Me Later keep it off). There is no GitHub account, no sign-in and no service of ours. Emetos stores no password, token or key, and writes none to a log.
- **Containers and Podman**: only the container engine you configured or that your Docker context, `DOCKER_HOST` or `CONTAINER_HOST` names (a Unix socket on this computer unless you named a `tcp://` address), and the registries your own `docker` or `podman` program contacts when you pull or push.
- **Databases** (SQL, MongoDB and Redis): only the servers of the connections you added, when you connect to them (and, for a keyring, the Secret Service on your own session bus). Result values and query text are never logged and no event carries them.
- **HTTP Client**: only the requests you send, to the URLs you wrote, and the authorization and token servers of an OAuth 2.0 login you started (your browser opens only when you click Get New Access Token). Nothing is synced to a server. Secret variables and OAuth tokens are in the desktop keyring (Secret Service, on your own session bus), never in a file; the history masks secrets and `Authorization` values. A client of a hosted workspace cannot run its commands.
- **Language servers and tools you install**: `gopls` and the `go` command may download modules as your Go settings say; "Go: Install/Update Tools" runs `go install` when you click it. The servers of [Languages](#languages) are programs you installed, run by Emetos on your machine with your user's rights: what they read and fetch is theirs (jdtls and csharp-ls restore Maven and NuGet packages, rust-analyzer runs `cargo check` and your build scripts, typescript-language-server may fetch type packages). Emetos itself sends nothing and installs no server.
- **Links**: a link in a notification opens in your browser only when you click it.

Logs stay on your disk (`$XDG_STATE_HOME/emetos/logs`) and are shown in the Output panel. The camera is read only while the Scan QR Code dialog is open, and no picture of it is kept or sent.

## Screenshots

Every screenshot of this README is made by `scripts/screenshots.sh` (it uses `scripts/proof.sh` in a headless compositor, with isolated settings and state, from the fixtures in `testdata/screenshots/`; the Remote Work shot runs two instances on `127.0.0.1`; the Git shots build a small repository of their own with the real `git`, in a neutral folder with its own configuration (`testdata/screenshots/git/build.sh`); the Run and Debug shot debugs a small Go program under Delve; the Testing shot runs the tests of a small Go project; the HTTP Client shot sends a request to the echo server that a debug build starts on `127.0.0.1`; the Containers shot runs over a fake Docker engine, and the MongoDB and Redis shots over throwaway servers in labelled containers on `127.0.0.1` that are removed afterwards). The rest:

![The side by side diff editor](docs/screenshots/diff.png)

![The merge editor with the incoming and current sides over the result](docs/screenshots/merge.png)

![The settings editor with a search](docs/screenshots/settings.png)

## Planned

Nothing is planned after M27: [the plan](docs/IMPLEMENTATION.md#m27-http-client) ends with it, so every milestone in it is built. What the sections above list as not built (and the `Planned, not built` line of [Status](#status)) is not scheduled, and none of it has a date.

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
