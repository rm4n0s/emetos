//go:build linux && !amd64 && !arm64

package realinput

// sysMemfdCreate is 0 where its number is not known here: sealedFile then uses a temporary file that is removed at once.
const sysMemfdCreate = 0
