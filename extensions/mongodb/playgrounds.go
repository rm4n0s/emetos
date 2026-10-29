package mongodb

import (
	"strings"

	"github.com/rm4n0s/emetos/extensions/mongodb/playground"
)

// playgroundText is the text of a new playground: a comment with the connection, the database to work on, and a first statement
// for a collection.
func playgroundText(connection, database, collection string) string {
	var b strings.Builder
	if connection != "" {
		b.WriteString("// " + oneLine(connection) + "\n")
	} else {
		b.WriteString("// MongoDB playground\n")
	}
	if database != "" {
		b.WriteString(useLine(database) + "\n\n")
	} else {
		b.WriteString("\n")
	}
	if collection != "" {
		b.WriteString("db.getCollection(" + playground.Quote(collection) + ").find({}).limit(20)\n")
	}
	return b.String()
}

// oneLine makes a name fit on a line of comment.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "\u2028", " ", "\u2029", " ").Replace(s)
}

// useLine is the statement that makes a database the current one. A name that "use" cannot read in its short form is written as a
// call.
func useLine(database string) string {
	if database == "" || strings.ContainsAny(database, " \t\r\n;/\\.\"$*<>:|?'") {
		return "use(" + playground.Quote(database) + ")"
	}
	return "use " + database
}

// collectionRef writes db.getCollection("name"), which works for any name.
func collectionRef(collection string) string {
	return "db.getCollection(" + playground.Quote(collection) + ")"
}

// createCollectionText is a playground that creates a collection, for the user to name and run.
func createCollectionText(database string) string {
	return "// Create a collection: write its name, then run this statement.\n" + useLine(database) + "\n\n" +
		"db.runCommand({ create: \"newCollection\" })\n"
}

// dropCollectionText is a playground that drops a collection. It is text for the user to read and run; nothing runs by itself.
func dropCollectionText(database, collection string) string {
	return "// Drops the collection " + oneLine(collection) + " and all of its documents. Read it, then run it.\n" + useLine(database) + "\n\n" +
		collectionRef(collection) + ".drop()\n"
}

// dropDatabaseText is a playground that drops a database.
func dropDatabaseText(database string) string {
	return "// Drops the database " + oneLine(database) + " with all of its collections. Read it, then run it.\n" + useLine(database) + "\n\n" +
		"db.runCommand({ dropDatabase: 1 })\n"
}

// createIndexText is a playground that creates an index on a collection, for the user to fill in.
func createIndexText(database, collection string) string {
	return "// Create an index: write the fields (1 ascending, -1 descending), then run this statement.\n" + useLine(database) + "\n\n" +
		collectionRef(collection) + ".createIndex({ field: 1 }, { name: \"field_1\" })\n"
}

// dropIndexText is a playground that drops an index.
func dropIndexText(database, collection, index string) string {
	return "// Drops the index " + oneLine(index) + " of " + oneLine(collection) + ". Read it, then run it.\n" + useLine(database) + "\n\n" +
		collectionRef(collection) + ".dropIndex(" + playground.Quote(index) + ")\n"
}
