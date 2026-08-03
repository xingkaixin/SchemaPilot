package database

import (
	"context"
	"errors"
	"testing"

	"github.com/schemapilot/schemapilot/internal/execution"
)

func TestFakeTargetTracksChecksumAndErrors(t *testing.T) {
	target := NewFakeTargetDatabase()
	application := execution.ScriptApplication{
		GraphName: "shop",
		NodeName:  "users",
		Path:      "users/001.sql",
		Checksum:  "abc",
		SQL:       "CREATE TABLE users (id INT)",
	}

	result, err := target.Apply(context.Background(), application)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != execution.ApplyOutcomeApplied {
		t.Fatalf("first outcome = %q", result.Outcome)
	}
	result, err = target.Apply(context.Background(), application)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != execution.ApplyOutcomeAlreadyApplied {
		t.Fatalf("second outcome = %q", result.Outcome)
	}

	application.Checksum = "def"
	_, err = target.Apply(context.Background(), application)
	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("checksum error = %v", err)
	}
	if mismatch.Recorded != "abc" || mismatch.Current != "def" {
		t.Fatalf("mismatch = %#v", mismatch)
	}

	application.Force = true
	result, err = target.Apply(context.Background(), application)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != execution.ApplyOutcomeApplied {
		t.Fatalf("forced outcome = %q", result.Outcome)
	}
	if got := target.AppliedScripts()["shop/users/users/001.sql"].Checksum; got != "def" {
		t.Fatalf("recorded checksum = %q", got)
	}
}

func TestFakeTargetPreconfiguredErrorDoesNotRecord(t *testing.T) {
	target := NewFakeTargetDatabase()
	expected := errors.New("script failed")
	target.SetScriptError("users/001.sql", expected)
	_, err := target.Apply(context.Background(), execution.ScriptApplication{
		GraphName: "shop",
		NodeName:  "users",
		Path:      "users/001.sql",
		Checksum:  "abc",
		SQL:       "CREATE TABLE users (id INT)",
	})
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v", err)
	}
	if len(target.AppliedScripts()) != 0 {
		t.Fatalf("failed script was recorded: %#v", target.AppliedScripts())
	}
	if events := target.Events(); len(events) != 1 || events[0].Error != expected.Error() {
		t.Fatalf("events = %#v", events)
	}
}
