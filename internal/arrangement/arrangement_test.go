package arrangement

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEncodeRoundTrips(t *testing.T) {
	original := Arrangement{
		Version: 1,
		Connections: map[string]Connection{
			"pg":    {Driver: "postgres", Steps: [][][]string{{{"pg/a.sql"}}, {{"pg/b.sql", "pg/c.sql"}, {"pg/d \"x\".sql"}}}, Disabled: []string{"pg/a.sql"}},
			"mysql": {Steps: [][][]string{}},
		},
		Detached: []string{"pg/e.sql"},
	}
	content, err := Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Arrangement
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("%v\n%s", err, content)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round trip mismatch\n got: %#v\nwant: %#v\n%s", decoded, original, content)
	}
}

func TestValidatePathRejectsEscapes(t *testing.T) {
	for _, path := range []string{"", "/etc/passwd", "../x.sql", "a/../../x.sql", "a\\b.sql", "./a.sql"} {
		if ValidatePath(path) == nil {
			t.Errorf("%q should be rejected", path)
		}
	}
	if err := ValidatePath("pg/sub/a.sql"); err != nil {
		t.Error(err)
	}
}
