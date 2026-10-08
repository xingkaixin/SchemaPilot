package config

import "testing"

func TestSaveLoadAndRename(t *testing.T) {
	dir := t.TempDir()
	if _, exists, err := Load(dir); err != nil || exists {
		t.Fatalf("missing file should load empty: %v %v", exists, err)
	}
	pg := Connection{Name: "pg-main", Driver: Postgres, Host: "127.0.0.1", Port: 5432, Database: "shop", User: "app", Password: "${PG_PASSWORD}", Params: map[string]string{"sslmode": "disable"}}
	my := Connection{Name: "mysql-report", Driver: MySQL, Host: "127.0.0.1"}
	config, err := Config{}.Upsert("", pg)
	if err != nil {
		t.Fatal(err)
	}
	if config, err = config.Upsert("", my); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Upsert("mysql-report", Connection{Name: "pg-main", Driver: MySQL, Host: "h"}); err == nil {
		t.Fatal("renaming onto an existing name should fail")
	}
	if config, err = config.Upsert("mysql-report", Connection{Name: "mysql-bi", Driver: MySQL, Host: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, config); err != nil {
		t.Fatal(err)
	}
	loaded, exists, err := Load(dir)
	if err != nil || !exists {
		t.Fatalf("load: %v %v", exists, err)
	}
	if names := loaded.Names(); len(names) != 2 || names[0] != "mysql-bi" || names[1] != "pg-main" {
		t.Fatalf("names %v", names)
	}
	if got, _ := loaded.Find("pg-main"); got.Password != "${PG_PASSWORD}" || got.Params["sslmode"] != "disable" {
		t.Fatalf("pg-main round trip: %+v", got)
	}
	if _, err := (Config{}).Upsert("", Connection{Name: "../x", Driver: Postgres, Host: "h"}); err == nil {
		t.Fatal("names must be safe directory names")
	}
}
