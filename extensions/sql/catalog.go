package sql

import (
	"context"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// catalogTTL is how long what the completion knows of a database is believed; a statement that changes the schema (CREATE, ALTER,
// DROP) makes it forget at once.
const catalogTTL = 5 * time.Minute

// catalogLoadTimeout bounds one read of the catalog for completion.
const catalogLoadTimeout = 10 * time.Second

// Catalog is what the completion of a query editor knows of the schema of one connection: the schemas, the tables and views of each,
// and the columns of the tables that were asked about. It reads the catalog with the dialect's queries the first time a thing is asked
// for and keeps the answer for a few minutes, so that typing in an editor never waits for the server twice. It is safe for use from
// several goroutines.
type Catalog struct {
	svc    *Service
	connID string
	d      dialect.Dialect

	mu      sync.Mutex
	schemas map[string]*catalogEntry[[]string]
	objects map[string]*catalogEntry[[]dialect.Object]
	columns map[string]*catalogEntry[[]dialect.Column]
}

// catalogEntry is a reading of the catalog that is under way or done.
type catalogEntry[T any] struct {
	done  chan struct{}
	value T
	err   error
	at    time.Time
}

// Catalog returns the catalog cache of an open connection; the same one every time until the connection closes.
func (s *Service) Catalog(connID string) (*Catalog, error) {
	d, err := s.Dialect(connID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.catalogs[connID]; c != nil {
		return c, nil
	}
	c := &Catalog{
		svc: s, connID: connID, d: d,
		schemas: map[string]*catalogEntry[[]string]{}, objects: map[string]*catalogEntry[[]dialect.Object]{}, columns: map[string]*catalogEntry[[]dialect.Column]{},
	}
	if s.catalogs == nil {
		s.catalogs = map[string]*Catalog{}
	}
	s.catalogs[connID] = c
	return c, nil
}

// Invalidate makes the catalog forget everything it read.
func (c *Catalog) Invalidate() {
	c.mu.Lock()
	clear(c.schemas)
	clear(c.objects)
	clear(c.columns)
	c.mu.Unlock()
}

// fetch returns the entry for the key, starting its reading when there is none or it is old, and waits for it. Several callers of one
// key share one reading.
func fetch[T any](ctx context.Context, mu *sync.Mutex, m map[string]*catalogEntry[T], key string, load func(context.Context) (T, error)) (T, error) {
	mu.Lock()
	e := m[key]
	if e == nil || (isDone(e.done) && (e.err != nil || time.Since(e.at) > catalogTTL)) {
		e = &catalogEntry[T]{done: make(chan struct{})}
		m[key] = e
		go func() {
			lctx, cancel := context.WithTimeout(context.Background(), catalogLoadTimeout)
			defer cancel()
			e.value, e.err = load(lctx)
			e.at = time.Now()
			close(e.done)
		}()
	}
	mu.Unlock()
	select {
	case <-e.done:
		return e.value, e.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// isDone tells whether the channel is closed.
func isDone(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Schemas lists the schemas of a database (of the connection, when the engine has one level: PostgreSQL's and SQL Server's schemas,
// SQLite's attached files). An engine with no schema level (MySQL) gives none.
func (c *Catalog) Schemas(ctx context.Context, database string) ([]string, error) {
	if !c.d.Levels().Schemas {
		return nil, nil
	}
	return fetch(ctx, &c.mu, c.schemas, database, func(ctx context.Context) ([]string, error) {
		rows, _, err := c.svc.Rows(ctx, c.connID, database, c.d.Schemas(database, false), 0)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, cell(r, 0))
		}
		return out, nil
	})
}

// Objects lists the tables and views of a schema (of a database, for MySQL).
func (c *Catalog) Objects(ctx context.Context, database, schema string) ([]dialect.Object, error) {
	ref := dialect.Ref{Database: database, Schema: schema}
	return fetch(ctx, &c.mu, c.objects, database+"\x00"+schema, func(ctx context.Context) ([]dialect.Object, error) {
		tables, _, err := c.svc.Rows(ctx, c.connID, database, c.d.Tables(ref, false), 0)
		if err != nil {
			return nil, err
		}
		views, _, err := c.svc.Rows(ctx, c.connID, database, c.d.Views(ref), 0)
		if err != nil {
			return nil, err
		}
		return append(dialect.DecodeTables(tables, ref), dialect.DecodeViews(views, ref)...), nil
	})
}

// Columns lists the columns of a table or view.
func (c *Catalog) Columns(ctx context.Context, ref dialect.Ref) ([]dialect.Column, error) {
	return fetch(ctx, &c.mu, c.columns, ref.Database+"\x00"+ref.Schema+"\x00"+ref.Name, func(ctx context.Context) ([]dialect.Column, error) {
		return c.svc.TableColumns(ctx, c.connID, c.d, ref)
	})
}
