# Handoff (2026-10-29)

Where the work on `docs/IMPLEMENTATION.md` stands, for continuing on another machine. Delete this file when the plan is finished.

## State

- **On `main` and tagged** (`step-<id>`): everything through M17c (already on GitHub before), M17d (17d.1 to 17d.7, the terminal), M17e (17e.1 to 17e.4), M18 (18.1 to 18.4, Docker and Podman), M19 (19.1 to 19.3, SQL) and 20.1 (MongoDB), with the fixes logged in `docs/IMPLEMENTED.md`. Published on 2026-10-29 because the user asked to continue from another PC. **The desktop pass of M17d and M17e (and of M18 to M20) on a real GPU has not been done yet** (see below).
- **Branches not merged yet** (pushed to GitHub):
  - `feat/step-21.1-redis`: step 21.1, rebased on `main` (`6afad78`), conflicts resolved, its real-server tests moved to `extensions/internal/dbtest`. Its full checks were stopped part-way: run them (`scripts/checks.sh`), then fast-forward `main` and tag `step-21.1`.
  - `feat/step-21.2-redis-console`: step 21.2, committed on the **old** 21.1 (`2902206`). Rebase it with `git rebase --onto feat/step-21.1-redis 2902206 feat/step-21.2-redis-console`, move its Docker tests to `dbtest` and `containerapitest` as 21.1's are, then the checks. It makes the console, Pub/Sub and Monitor one panel tab "Redis" (decision 319) because many panel tabs left the Problems filter no room.
- **Not started:** 20.2 (needs 19.3's results panel and 20.1, both on `main`), 22.1, 22.2 (the README).

## To do besides the steps

- **Desktop pass** before a milestone is reported done: `GLUTTER_RENDERER=auto EMETOS_REALINPUT_RACE=0 scripts/realinput.sh --desktop` and `scripts/terminal-perf.sh --desktop` on the user's GPU (a visible nested sway window), and look at the screenshots.
- **The panel's title strip** (`internal/gui/workbench/workbench.go`, `panel`): the tabs get the room first and the view's actions what is left, so with many extension panel tabs the Problems filter disappears at 1280 px. VS Code does the opposite (the actions keep their room, the tabs go into "..."). Change it, log the decision, and remove the test workarounds of 18.4 and 21.2 (`overflow && width > 1024`, closing the side bar in `TestRealInputProblemsFilter`).
- **A flaky test:** `extensions/remotework` `TestShowMyPublicKeyOfTheAgent/several_keys_and_none_chosen` fails under load now and then.
- **Decisions to show the user:** 153, 174, 210, 219, 301, 303, 347 (in `docs/DECISIONS-TO-REVIEW.md`).
- **Rebuild the user's binary** after the desktop pass: `scripts/heavy.sh -- go build -tags glutter_release -o emetos ./cmd/emetos`.

## Rules learned on the way (also follow CLAUDE.md)

- Never accept third-party terms for the user: no `ACCEPT_EULA` or the like, no SQL Server container (SQL Server is tested against fakes); decide conservatively and log it.
- Never use, read or port xterm.js or another terminal's code; the terminal emulator is Emetos's own.
- Temporary files go under `/tmp` or `/var/tmp`, never under the home folder; tests isolate `XDG_*` and `HOME`.
- `/tmp` is a tmpfs: Go's build scratch of long runs goes to `GOTMPDIR=/var/tmp/emetos-go` (`scripts/checks.sh` does it), else killed runs leave gigabytes in RAM.
- When the user needs the machine's RAM (they run a VM), stop everything first: agents, and the detached check runs by process group. Start a heavy run only with 4 GB or more available.
- Read RACE-EXIT, NOCGO-EXIT and REAL-EXIT in a separate step before every merge, and run the full suite also for a test-only fix.
- No real IP addresses in the repository or in proof images: RFC 5737 documentation addresses.
- Never delete the user's data; the Notes and Scratchpad text of 17e.1 was exported to `$XDG_DATA_HOME/emetos/saved-notes/`.
- Container tests never touch the user's own containers, images, networks or volumes (only `dev.emetos.test=1` labelled ones).
- Parallel steps each get their own worktree, branch and range of decision numbers; the manager merges in the plan's order, one at a time.
