package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/schemapilot/schemapilot/internal/migration"
)

func TestRedactedDatabaseErrorDoesNotExposeDSNSecrets(t *testing.T) {
	dsn := "postgres://alice:super-secret@db.example.com/app"
	cause := errors.New("dial failed for " + dsn + " (password=super-secret)")
	err := redactDatabaseError("ping", migration.DriverPostgres, dsn, cause)
	if strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), dsn) {
		t.Fatalf("redacted error leaks credentials: %v", err)
	}
	if errors.Unwrap(err) != nil {
		t.Fatalf("redacted error exposes an unsanitized unwrap cause")
	}
}

func TestRedactedDatabaseErrorPreservesCancellationClassification(t *testing.T) {
	err := redactDatabaseError("ping", migration.DriverPostgres, "postgres://localhost/app", context.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline classification was lost: %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("deadline was classified as cancellation")
	}
}
