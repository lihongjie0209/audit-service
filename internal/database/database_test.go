package database

import "testing"

func TestMySQLConnectionConfigSetsClientAndSessionTimezone(t *testing.T) {
	t.Parallel()
	configuration, err := mysqlConnectionConfig("user:pass@tcp(localhost:3306)/old", "audit")
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.ParseTime || configuration.Loc.String() != "Asia/Shanghai" || configuration.Params["time_zone"] != "'+08:00'" || configuration.DBName != "audit" {
		t.Fatalf("mysql config = %+v", configuration)
	}
}
