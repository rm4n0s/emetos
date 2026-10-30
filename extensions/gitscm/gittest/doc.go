// Package gittest is the test harness of the Git extension (SPEC §10.30 "Tests", D36): it runs the real git program in
// temporary, isolated repositories. Everything a test starts gets an environment made from scratch: its own HOME and XDG
// folders, empty global and system configuration files, GIT_CONFIG_NOSYSTEM, a ceiling directory above the temporary
// folder, no terminal prompts, a test identity (Ada Example <ada@example.com>) and fixed dates, so that the user's git
// configuration, credentials, SSH keys and repositories are never read or written and the hashes of the repositories a
// test makes are stable. Nothing here touches the network: remotes are local bare repositories, or the local server of
// HTTPBackend on 127.0.0.1.
//
// A test that needs git calls Require (it skips when git is missing or older than 2.30) or New (which calls Require, makes
// a repository and trusts it). The package imports neither the testing package nor the Git client, so the fixture program
// (cmd/gitfixture) can use it and the tests of the client can import it.
package gittest
