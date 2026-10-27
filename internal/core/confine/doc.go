// Package confine reads a folder for someone who may see that folder and nothing else (SPEC §10.19 "Confinement"): the
// files topics of the core, and the check that a document belongs to the workspace. A path is relative to the folder, with
// forward slashes; ".." and absolute paths are refused before the disk is touched, and the rest is left to os.Root, which
// resolves every component beneath an open directory (openat2 with RESOLVE_BENEATH on Linux), so a symbolic link that
// points out of the folder, a link swapped in while a call runs and a folder that is itself reached through a link cannot
// lead outside. Absolute links are refused even when they point inside. The package imports no UI and no Emetos package but
// the public contract.
package confine
