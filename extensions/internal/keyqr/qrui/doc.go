// Package qrui is the part of Public keys by QR code (SPEC §10.18, step 15.5) that the user sees, used by Remote Work:
// the Scan QR Code dialog, with the live picture of a camera, a picker when there are several and the confirmation of
// the key that was read, and the Show My Public Key as QR Code dialog.
//
// A dialog is not a window of its own: it is a widget that its owner (a page, a window tab) stacks over its content with Build
// while the dialog is open, and rebuilds through the Refresh function it was given when the dialog changes. The owner makes it
// when the user presses the button, drops it in the Close function it gave, and so keeps no state of the dialog.
//
// Everything runs on the UI thread except the goroutine that reads the camera, which hands its results over with the Post
// function of the extension API. The camera is opened when the dialog opens and released when it closes, or as soon as a key was
// read; no frame is kept past the picture on screen, and nothing is sent anywhere.
package qrui
