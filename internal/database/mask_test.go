package database

import "testing"

func TestMaskDSN(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "postgres URL",
			raw:  "postgres://alice:secret@db.example.com/app?sslmode=require",
			want: "postgres://alice:%2A%2A%2A@db.example.com/app?sslmode=require",
		},
		{
			name: "sqlserver URL query password",
			raw:  "sqlserver://alice:secret@db.example.com?database=app&password=query-secret",
			want: "sqlserver://alice:%2A%2A%2A@db.example.com?database=app&password=%2A%2A%2A",
		},
		{
			name: "mysql DSN",
			raw:  "alice:secret@tcp(db.example.com:3306)/app?parseTime=true",
			want: "alice:***@tcp(db.example.com:3306)/app?parseTime=true",
		},
		{
			name: "empty",
			raw:  "",
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MaskDSN(test.raw); got != test.want {
				t.Fatalf("MaskDSN(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}
