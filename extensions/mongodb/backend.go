package mongodb

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// CollectionKind is what a collection of the server is.
type CollectionKind string

// The kinds of collection.
const (
	// KindCollection is an ordinary collection.
	KindCollection CollectionKind = "collection"
	// KindView is a view: a pipeline over another collection.
	KindView CollectionKind = "view"
	// KindTimeSeries is a time series collection.
	KindTimeSeries CollectionKind = "timeseries"
)

// DatabaseInfo is a database of the server.
type DatabaseInfo struct {
	Name string
	// SizeOnDisk is in bytes; Empty says the database has no data.
	SizeOnDisk int64
	Empty      bool
}

// CollectionInfo is a collection of a database.
type CollectionInfo struct {
	Name string
	Kind CollectionKind
	// ViewOn is the collection a view reads from.
	ViewOn string
}

// IndexInfo is an index of a collection.
type IndexInfo struct {
	Name string
	// Keys are the indexed fields in order, with 1, -1 or the name of a special index ("text", "2dsphere", "hashed").
	Keys bson.D
	// Unique and Sparse are the flags of the index; TTLSeconds is the expireAfterSeconds of a TTL index, or nil.
	Unique, Sparse bool
	TTLSeconds     *int32
}

// Backend is what the explorer needs of one open server: the driver behind an interface, so that the explorer is tested against a
// fake (SPEC §10.22). Every call takes a context and ends with it.
type Backend interface {
	// Databases lists the databases the user may see.
	Databases(ctx context.Context) ([]DatabaseInfo, error)
	// Collections lists the collections, views and time series collections of a database.
	Collections(ctx context.Context, database string) ([]CollectionInfo, error)
	// EstimatedCount is the estimated number of documents of a collection, from its metadata.
	EstimatedCount(ctx context.Context, database, collection string) (int64, error)
	// Indexes lists the indexes of a collection.
	Indexes(ctx context.Context, database, collection string) ([]IndexInfo, error)
	// Sample reads up to n documents chosen at random from a collection or a view.
	Sample(ctx context.Context, database, collection string, n int) ([]bson.D, error)
	// Exec runs a call of a playground (SPEC §10.22 "Playgrounds"). A call that reads with a cursor answers with its first page of at
	// most pageSize documents and the cursor that reads the rest, if there is a rest; any other call answers with all of its
	// documents. A failure of the server has the code and the name of the code in the metadata of its error (ServerError).
	Exec(ctx context.Context, call Call, pageSize int) (Reply, error)
	// Close ends the connection to the server.
	Close(ctx context.Context) error
}

// Dialer opens a Backend. The real one speaks to a server with the official driver; the tests give a fake.
type Dialer interface {
	// Dial connects as the connection says, with the password (empty for none, already found by the hub), and checks that the
	// server answers.
	Dial(ctx context.Context, c dbconn.Connection, password string) (Backend, error)
}
