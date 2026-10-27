// Package keyform is the choice of the key a client logs in with, shared by the connect dialog of Remote Work and the Add Host form of
// Relay (SPEC §10.18 "As built (step 15.4)", §10.19 "As built (step 16.3)"): a private key file (a box and the dialog of the
// desktop) or a key of the ssh-agent (listed off the UI thread), the line of the public key that Show My Public Key as QR Code draws,
// and the controls that show all of it. It holds no passphrase and no key: the choice is a path or a fingerprint.
package keyform
