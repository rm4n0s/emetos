package assets

import (
	"embed"
	"io/fs"
)

// tutorFiles are the lessons of the Emetos Tutor (SPEC §10.35) under the folder "tutor".
//
//go:embed tutor
var tutorFiles embed.FS

// TutorRoot is the folder of Tutor() that holds the lessons.
const TutorRoot = "tutor"

// Tutor gives the lessons of the Emetos Tutor: TUTOR.md and the practice files, under TutorRoot. The files are read
// only; the tutor works on a copy of them.
func Tutor() fs.FS { return tutorFiles }
