package mongodb

import (
	"context"
	"log/slog"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// driverDialer is the Dialer that speaks to a server with the official driver. timeout is how long it looks for the server (the
// default when zero).
type driverDialer struct{ timeout time.Duration }

// Dial implements Dialer: it opens the client, pings the server (so that a wrong address, a wrong password or a server that is not
// there fails now, not at the first use) and returns the Backend over it.
func (d driverDialer) Dial(ctx context.Context, c dbconn.Connection, password string) (Backend, error) {
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	opts, err := clientOptions(c, password, timeout)
	if err != nil {
		return nil, err
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, errors.NewErr("MongoConnectFailed", err).SetMetadata("connection", c.Name)
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer closeCancel()
		if derr := client.Disconnect(closeCtx); derr != nil {
			slog.Warn("a client that failed to connect did not close", "connection", c.Name, "err", derr)
		}
		return nil, errors.NewErr("MongoConnectFailed", err).SetMetadata("connection", c.Name)
	}
	return &driverBackend{client: client}, nil
}

// driverBackend is a Backend over a client of the driver.
type driverBackend struct{ client *mongo.Client }

// Databases implements Backend.
func (b *driverBackend) Databases(ctx context.Context) ([]DatabaseInfo, error) {
	res, err := b.client.ListDatabases(ctx, bson.D{}, options.ListDatabases().SetAuthorizedDatabases(true))
	if err != nil {
		return nil, errors.NewErr("MongoListDatabasesFailed", err)
	}
	out := make([]DatabaseInfo, len(res.Databases))
	for i, d := range res.Databases {
		out[i] = DatabaseInfo{Name: d.Name, SizeOnDisk: d.SizeOnDisk, Empty: d.Empty}
	}
	return out, nil
}

// Collections implements Backend.
func (b *driverBackend) Collections(ctx context.Context, database string) ([]CollectionInfo, error) {
	specs, err := b.client.Database(database).ListCollectionSpecifications(ctx, bson.D{})
	if err != nil {
		return nil, errors.NewErr("MongoListCollectionsFailed", err).SetMetadata("database", database)
	}
	out := make([]CollectionInfo, len(specs))
	for i, s := range specs {
		info := CollectionInfo{Name: s.Name, Kind: KindCollection}
		switch s.Type {
		case "view":
			info.Kind = KindView
			if v, err := s.Options.LookupErr("viewOn"); err == nil {
				info.ViewOn, _ = v.StringValueOK()
			}
		case "timeseries":
			info.Kind = KindTimeSeries
		}
		out[i] = info
	}
	return out, nil
}

// EstimatedCount implements Backend.
func (b *driverBackend) EstimatedCount(ctx context.Context, database, collection string) (int64, error) {
	n, err := b.client.Database(database).Collection(collection).EstimatedDocumentCount(ctx)
	if err != nil {
		return 0, errors.NewErr("MongoCountFailed", err).SetMetadata("database", database, "collection", collection)
	}
	return n, nil
}

// Indexes implements Backend.
func (b *driverBackend) Indexes(ctx context.Context, database, collection string) ([]IndexInfo, error) {
	specs, err := b.client.Database(database).Collection(collection).Indexes().ListSpecifications(ctx)
	if err != nil {
		return nil, errors.NewErr("MongoListIndexesFailed", err).SetMetadata("database", database, "collection", collection)
	}
	out := make([]IndexInfo, len(specs))
	for i, s := range specs {
		var keys bson.D
		if err := bson.Unmarshal(s.KeysDocument, &keys); err != nil {
			return nil, errors.NewErr("MongoListIndexesFailed", err).SetMetadata("database", database, "collection", collection, "index", s.Name)
		}
		info := IndexInfo{Name: s.Name, Keys: keys, TTLSeconds: s.ExpireAfterSeconds}
		info.Unique = s.Unique != nil && *s.Unique
		info.Sparse = s.Sparse != nil && *s.Sparse
		out[i] = info
	}
	return out, nil
}

// Sample implements Backend: a $sample stage picks the documents at the server, so that nothing more than n documents is sent.
func (b *driverBackend) Sample(ctx context.Context, database, collection string, n int) ([]bson.D, error) {
	pipeline := mongo.Pipeline{{{Key: "$sample", Value: bson.D{{Key: "size", Value: n}}}}}
	cur, err := b.client.Database(database).Collection(collection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, errors.NewErr("MongoSampleFailed", err).SetMetadata("database", database, "collection", collection)
	}
	var docs []bson.D
	if err := cur.All(ctx, &docs); err != nil {
		return nil, errors.NewErr("MongoSampleFailed", err).SetMetadata("database", database, "collection", collection)
	}
	return docs, nil
}

// Close implements Backend.
func (b *driverBackend) Close(ctx context.Context) error {
	if err := b.client.Disconnect(ctx); err != nil {
		return errors.NewErr("MongoDisconnectFailed", err)
	}
	return nil
}
