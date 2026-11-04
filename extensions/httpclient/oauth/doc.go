// Package oauth gets, keeps and refreshes the OAuth 2.0 tokens of the HTTP client (SPEC §10.33 "OAuth 2.0"): the client credentials
// grant (RFC 6749 §4.4) and the authorization code grant (§4.1) with PKCE (RFC 7636, S256), whose code arrives on a loopback redirect
// (RFC 8252 §7.3) that exists only while it waits. Tokens are kept in the desktop keyring, never in a file or in the state of the
// window (D45). The package opens no browser itself: the caller gives it an Opener.
package oauth
