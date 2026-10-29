package sql

import (
	"testing"
	"time"
)

func TestAPostgreSQLTimestampWithZoneIsShownInUTC(t *testing.T) {
	zone := time.FixedZone("EEST", 3*3600)
	at := time.Date(2026, 10, 29, 9, 7, 8, 0, zone)
	for _, tc := range []struct {
		name, engine, typeName, want string
	}{
		{"timestamptz of PostgreSQL", "postgres", "TIMESTAMPTZ", "2026-10-29T06:07:08Z"},
		{"datetimeoffset of SQL Server keeps its offset", "sqlserver", "DATETIMEOFFSET", "2026-10-29T09:07:08+03:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := convert(tc.engine, tc.typeName, at).Full("NULL"); got != tc.want {
				t.Errorf("convert = %q, want %q", got, tc.want)
			}
		})
	}
}
