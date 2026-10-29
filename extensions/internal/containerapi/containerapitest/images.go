package containerapitest

// The images the tests against the real engines may pull, in one place (CLAUDE.md "Testing notes": only pinned image tags
// listed in one place). Each is a full reference with a version tag that does not move in practice, never "latest". The
// harness refuses to pull anything that is not listed here.
const (
	// ImageBusyBox is the small image the real-engine tests run containers from (a few megabytes). The tag is BusyBox 1.37.0.
	ImageBusyBox = "docker.io/library/busybox:1.37.0"

	// The servers of the tests of the SQL extension (extensions/internal/dbtest, SPEC §10.21). SQL Server is not among them: its image
	// starts only when Microsoft's license terms are accepted, which a test never does on the user's behalf.

	// ImagePostgres is PostgreSQL 17.6 on Alpine.
	ImagePostgres = "docker.io/library/postgres:17.6-alpine"
	// ImageMySQL is MySQL 8.4.6 (a long-term support release).
	ImageMySQL = "docker.io/library/mysql:8.4.6"
	// ImageMariaDB is MariaDB 11.4.8 (a long-term support release).
	ImageMariaDB = "docker.io/library/mariadb:11.4.8"

	// ImageMongoDB is MongoDB 7.0.14, the server of the tests of the MongoDB extension (extensions/mongodb, SPEC §10.22): 7.0 has
	// time series collections and the $sample stage, and its image starts a server with a root user from the environment.
	ImageMongoDB = "docker.io/library/mongo:7.0.14"
)

// PinnedImages lists every image the real-engine tests may pull.
var PinnedImages = []string{ImageBusyBox, ImagePostgres, ImageMySQL, ImageMariaDB, ImageMongoDB}

// IsPinned reports whether ref is one of PinnedImages.
func IsPinned(ref string) bool {
	for _, p := range PinnedImages {
		if p == ref {
			return true
		}
	}
	return false
}
