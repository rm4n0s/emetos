package sql

import (
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
	"github.com/rm4n0s/emetos/extensions/sql/dialect"
)

// The names of the options (dbconn.Connection.Options) the SQL engines have.
const (
	// OptionShowSystem makes the explorer list the databases and schemas the engine keeps for itself ("true").
	OptionShowSystem = "showSystem"
	// OptionConnectTimeout is how long a connection may take to open, in seconds.
	OptionConnectTimeout = "connectTimeout"
	// OptionInstance is the named instance of a SQL Server.
	OptionInstance = "instance"
	// OptionCreate lets SQLite create the file when it is not there ("true").
	OptionCreate = "create"
)

// defaultConnectTimeout is the seconds a connection may take when the connection does not say.
const defaultConnectTimeout = 15

var (
	showSystemField = dbconn.Field{Key: OptionShowSystem, Label: "Show system databases and schemas", Kind: dbconn.FieldBool, Default: "false"}
	timeoutField    = dbconn.Field{Key: OptionConnectTimeout, Label: "Connect timeout (seconds)", Kind: dbconn.FieldNumber, Default: "15"}
)

// Engines returns the engines of the SQL extension in the order the dialects are listed: how the forms of PostgreSQL, MySQL and
// MariaDB, SQLite and SQL Server look.
func Engines() []dbconn.Engine {
	return []dbconn.Engine{
		{
			ID: "postgres", Name: dialect.Postgres{}.Name(), Icon: "database", DefaultPort: 5432, URLSchemes: []string{"postgres", "postgresql"},
			Fields: []dbconn.Field{showSystemField, timeoutField},
		},
		{
			ID: "mysql", Name: dialect.MySQL{}.Name(), Icon: "database", DefaultPort: 3306, URLSchemes: []string{"mysql", "mariadb"},
			Fields: []dbconn.Field{showSystemField, timeoutField},
		},
		{
			ID: "sqlite", Name: dialect.SQLite{}.Name(), Icon: "database", File: true, NoUser: true, NoDatabase: true, NoTLS: true, NoPassword: true,
			Fields: []dbconn.Field{{Key: OptionCreate, Label: "Create the file if it does not exist", Kind: dbconn.FieldBool, Default: "false"}},
		},
		{
			ID: "sqlserver", Name: dialect.SQLServer{}.Name(), Icon: "database", DefaultPort: 1433, URLSchemes: []string{"sqlserver"},
			Fields: []dbconn.Field{
				{Key: OptionInstance, Label: "Named instance", Kind: dbconn.FieldText, Placeholder: "SQLEXPRESS"},
				showSystemField, timeoutField,
			},
		},
	}
}
