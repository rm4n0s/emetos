//go:build emetos_display

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The MongoDB playgrounds and their results as a user meets them (step 20.2, SPEC §10.22), against a real server. The test needs
// a MongoDB that the tester started: a throwaway container of the pinned image with the label of the tests, on the loopback
// address, whose port is in EMETOS_PROOF_MONGO_PORT, for example
//
//	docker run -d --rm --name emetos-test-proof-mongo --label dev.emetos.test=1 -p 127.0.0.1:27099:27017 mongo:7.0.14
//	EMETOS_PROOF_MONGO_PORT=27099 EMETOS_PROOF_DIR=... scripts/heavy.sh --alone -- go test -tags emetos_display ./cmd/emetos -run TestRealInputMongoDBPlayground
//	docker rm -f emetos-test-proof-mongo
//
// Without the variable the test is skipped. It seeds the server itself and leaves nothing on it that matters (the container is
// removed by the tester).
func TestRealInputMongoDBPlayground(t *testing.T) {
	port := os.Getenv("EMETOS_PROOF_MONGO_PORT")
	if port == "" {
		t.Skip("EMETOS_PROOF_MONGO_PORT names no MongoDB to run against")
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("EMETOS_PROOF_MONGO_PORT = %q: %v", port, err)
	}
	seedProofMongo(t, port)
	conns, err := json.Marshal(map[string]any{"version": 1, "connections": []map[string]any{
		{"id": "mongo1", "name": "Shop", "engine": "mongodb", "scope": "user", "host": "localhost", "port": n, "database": "shop", "color": "#13aa52",
			"options": map[string]string{"directConnection": "true"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	a := display.Start(t, display.Options{
		Args:  []string{".", "notes.txt"},
		State: map[string]string{"db-connections.json": string(conns)},
		Project: map[string]string{
			"notes.txt": "playgrounds live in the menu of the Databases view\n",
		},
	})
	a.Settle()
	a.Press("ctrl+shift+p")
	a.Type("Show Databases")
	a.Eventually("the palette to list the command", func() (bool, string) {
		for _, tx := range a.Texts() {
			if tx.Text == "Show Databases" || tx.Text == "View: Show Databases" {
				return true, tx.Text
			}
		}
		return false, a.Dump()
	})
	a.Press("enter")
	a.WaitText("Shop")
	a.RightClickText("Shop")
	a.WaitText("Connect")
	a.ClickText("Connect")
	// the server has no password: Enter for none
	a.Eventually("the password box", func() (bool, string) {
		for _, tx := range a.Texts() {
			if strings.HasPrefix(tx.Text, "Password for") {
				return true, tx.Text
			}
		}
		return false, a.Dump()
	})
	a.Press("enter")
	open := func(label, wantBelow string) {
		a.Eventually("the row "+label+" to open", func() (bool, string) {
			if _, ok := a.Text(wantBelow); ok {
				return true, ""
			}
			if tx, ok := a.Text(label); ok {
				a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
				a.Press("right")
			}
			return false, a.Dump()
		})
	}
	open("Shop", "shop")
	open("shop", "orders")
	// a playground for the collection, bound to the connection and the database
	orders := a.WaitText("orders")
	a.RightClick(orders.X+orders.Width/2, orders.Y+orders.Height/2)
	a.WaitText("New Playground")
	a.ClickText("New Playground")
	a.WaitText(" Shop · shop")
	a.Settle()
	a.Press("ctrl+shift+enter")
	a.WaitText("orders.find (3 documents)")
	a.Eventually("the documents to show", func() (bool, string) {
		_, ok := a.Text("#1")
		return ok, a.Dump()
	})
	one := a.WaitText("#1")
	a.Click(one.X+one.Width/2, one.Y+one.Height/2)
	a.WaitText("customer")
	cust := a.WaitText("customer")
	a.Click(cust.X+cust.Width/2, cust.Y+cust.Height/2)
	a.WaitText("name")
	a.Settle()
	a.Screenshot(filepath.Join(proofDir(t), "20.2-mongodb-playground.png"))
}

// seedProofMongo fills the server with the orders the proof shows.
func seedProofMongo(t *testing.T, port string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client, err := mongo.Connect(mongooptions.Client().ApplyURI("mongodb://localhost:" + port + "/?directConnection=true"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	orders := client.Database("shop").Collection("orders")
	if err := orders.Drop(ctx); err != nil {
		t.Fatal(err)
	}
	day := func(d int) bson.DateTime {
		return bson.NewDateTimeFromTime(time.Date(2026, 10, d, 9, 30, 0, 0, time.UTC))
	}
	docs := []any{
		bson.D{{Key: "sku", Value: "A1"}, {Key: "qty", Value: int32(2)}, {Key: "price", Value: 9.5}, {Key: "paid", Value: true}, {Key: "created", Value: day(29)},
			{Key: "customer", Value: bson.D{{Key: "name", Value: "Ann"}, {Key: "address", Value: bson.D{{Key: "city", Value: "Athens"}}}}}, {Key: "tags", Value: bson.A{"new", "gift"}}},
		bson.D{{Key: "sku", Value: "B2"}, {Key: "qty", Value: int32(5)}, {Key: "price", Value: 20.0}, {Key: "paid", Value: false}, {Key: "created", Value: day(28)},
			{Key: "customer", Value: bson.D{{Key: "name", Value: "Bob"}}}},
		bson.D{{Key: "sku", Value: "C3"}, {Key: "qty", Value: int32(1)}, {Key: "price", Value: 4.25}, {Key: "paid", Value: true}, {Key: "created", Value: day(27)}},
	}
	if _, err := orders.InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
}
