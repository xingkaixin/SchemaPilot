package runstore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

func TestStoreCreateAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.db")
	definition := testDefinition("run-create", migration.RunStatusPending, time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC))

	store := openTestStore(t, path)
	if err := store.Create(context.Background(), definition); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	snapshot, err := store.Snapshot(context.Background(), definition.Run.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Run.GraphName != "shop" || len(snapshot.Nodes) != 3 {
		t.Fatalf("unexpected initial snapshot: %+v", snapshot)
	}
	if snapshot.Nodes[2].DependsOn[0] != "user" || snapshot.Nodes[2].Scripts[0].Checksum != "report-hash" {
		t.Fatalf("graph metadata was not persisted: %+v", snapshot.Nodes[2])
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store = openTestStore(t, path)
	t.Cleanup(func() { store.Close() })
	snapshot, err = store.Snapshot(context.Background(), definition.Run.ID)
	if err != nil {
		t.Fatalf("Snapshot() after reopen error = %v", err)
	}
	if snapshot.Run.ID != definition.Run.ID || snapshot.Run.GraphFingerprint != "graph-hash" {
		t.Fatalf("run did not persist across reopen: %+v", snapshot.Run)
	}
}

func TestStoreCompleteRun(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "runs.db"))
	defer store.Close()
	definition := testDefinition("run-success", migration.RunStatusPending, time.Now().UTC())
	createTestRun(t, store, definition)

	at := time.Now().UTC()
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptStarted, RunID: definition.Run.ID, Attempt: 1, GraphFingerprint: "graph-hash", RunStatus: migration.RunStatusRunning, At: at})
	for _, node := range definition.Graph.Nodes {
		applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionNodeStarted, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, NodeStatus: migration.NodeStatusRunning, At: at.Add(time.Second)})
		for _, script := range node.Scripts {
			applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionScriptStarted, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, Script: script.Path, Checksum: script.Checksum, ScriptStatus: migration.ScriptStatusRunning, At: at.Add(2 * time.Second)})
			applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionScriptFinished, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, Script: script.Path, ScriptStatus: migration.ScriptStatusSucceeded, RowsAffected: 7, At: at.Add(3 * time.Second)})
		}
		applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionNodeFinished, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, NodeStatus: migration.NodeStatusSucceeded, At: at.Add(4 * time.Second)})
	}
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptFinished, RunID: definition.Run.ID, Attempt: 1, RunStatus: migration.RunStatusSucceeded, At: at.Add(5 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionRunFinished, RunID: definition.Run.ID, Attempt: 1, RunStatus: migration.RunStatusSucceeded, At: at.Add(6 * time.Second)})

	snapshot, err := store.Snapshot(context.Background(), definition.Run.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Run.Status != migration.RunStatusSucceeded || len(snapshot.Attempts) != 1 {
		t.Fatalf("unexpected run state: %+v", snapshot)
	}
	for _, node := range snapshot.Nodes {
		if node.Status != migration.NodeStatusSucceeded || node.Attempt != 1 {
			t.Fatalf("unexpected node state: %+v", node)
		}
		for _, script := range node.Scripts {
			if script.Status != migration.ScriptStatusSucceeded || script.RowsAffected != 7 || script.Attempt != 1 {
				t.Fatalf("unexpected script state: %+v", script)
			}
		}
	}
}

func TestStoreResumePreservesAttemptHistoryAndProjectsLatest(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "runs.db"))
	defer store.Close()
	definition := testDefinition("run-resume", migration.RunStatusPending, time.Now().UTC())
	createTestRun(t, store, definition)

	at := time.Now().UTC()
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptStarted, RunID: definition.Run.ID, Attempt: 1, GraphFingerprint: "graph-hash", RunStatus: migration.RunStatusRunning, At: at})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionNodeStarted, RunID: definition.Run.ID, Attempt: 1, Node: "user", NodeStatus: migration.NodeStatusRunning, At: at.Add(time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionScriptStarted, RunID: definition.Run.ID, Attempt: 1, Node: "user", Script: "user/001.sql", Checksum: "user-hash", ScriptStatus: migration.ScriptStatusRunning, At: at.Add(2 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionScriptFinished, RunID: definition.Run.ID, Attempt: 1, Node: "user", Script: "user/001.sql", ScriptStatus: migration.ScriptStatusFailed, Error: "duplicate column", At: at.Add(3 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionNodeFinished, RunID: definition.Run.ID, Attempt: 1, Node: "user", NodeStatus: migration.NodeStatusFailed, Error: "duplicate column", At: at.Add(4 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptFinished, RunID: definition.Run.ID, Attempt: 1, RunStatus: migration.RunStatusFailed, Error: "duplicate column", At: at.Add(5 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionRunFinished, RunID: definition.Run.ID, Attempt: 1, RunStatus: migration.RunStatusFailed, Error: "duplicate column", At: at.Add(6 * time.Second)})

	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptStarted, RunID: definition.Run.ID, Attempt: 2, GraphFingerprint: "graph-hash-v2", RunStatus: migration.RunStatusRunning, At: at.Add(7 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionNodeStarted, RunID: definition.Run.ID, Attempt: 2, Node: "user", NodeStatus: migration.NodeStatusRunning, At: at.Add(8 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionScriptStarted, RunID: definition.Run.ID, Attempt: 2, Node: "user", Script: "user/001.sql", Checksum: "user-hash-v2", ScriptStatus: migration.ScriptStatusRunning, At: at.Add(9 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionScriptFinished, RunID: definition.Run.ID, Attempt: 2, Node: "user", Script: "user/001.sql", ScriptStatus: migration.ScriptStatusSucceeded, At: at.Add(10 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionNodeFinished, RunID: definition.Run.ID, Attempt: 2, Node: "user", NodeStatus: migration.NodeStatusSucceeded, At: at.Add(11 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptFinished, RunID: definition.Run.ID, Attempt: 2, RunStatus: migration.RunStatusSucceeded, At: at.Add(12 * time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionRunFinished, RunID: definition.Run.ID, Attempt: 2, RunStatus: migration.RunStatusSucceeded, At: at.Add(13 * time.Second)})

	snapshot, err := store.Snapshot(context.Background(), definition.Run.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Attempts) != 2 || snapshot.Run.GraphFingerprint != "graph-hash-v2" || snapshot.Attempts[0].Status != migration.RunStatusFailed || snapshot.Attempts[1].Status != migration.RunStatusSucceeded || snapshot.Attempts[1].GraphFingerprint != "graph-hash-v2" {
		t.Fatalf("attempt history was not preserved: %+v", snapshot.Attempts)
	}
	if snapshot.Nodes[0].Attempt != 2 || snapshot.Nodes[0].Status != migration.NodeStatusSucceeded || snapshot.Nodes[0].Scripts[0].Attempt != 2 || snapshot.Nodes[0].Scripts[0].Checksum != "user-hash-v2" {
		t.Fatalf("latest projection is wrong: %+v", snapshot.Nodes[0])
	}
}

func TestStoreRunsFilterAndLimit(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "runs.db"))
	defer store.Close()
	for index, status := range []migration.RunStatus{migration.RunStatusPending, migration.RunStatusFailed, migration.RunStatusSucceeded} {
		definition := testDefinition(fmt.Sprintf("run-%d", index), status, time.Now().UTC().Add(time.Duration(index)*time.Second))
		createTestRun(t, store, definition)
	}

	runs, err := store.Runs(context.Background(), execution.RunQuery{Statuses: []migration.RunStatus{migration.RunStatusFailed, migration.RunStatusSucceeded}, Limit: 1})
	if err != nil {
		t.Fatalf("Runs() error = %v", err)
	}
	if len(runs) != 1 || runs[0].Run.Status != migration.RunStatusSucceeded {
		t.Fatalf("filter/limit mismatch: %+v", runs)
	}
}

func TestStoreConcurrentNodeAndLogWrites(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "runs.db"))
	defer store.Close()
	definition := testDefinition("run-concurrent", migration.RunStatusPending, time.Now().UTC())
	definition.Graph.Nodes = make([]migration.Node, 0, 12)
	for index := 0; index < 12; index++ {
		definition.Graph.Nodes = append(definition.Graph.Nodes, migration.Node{
			Name:     fmt.Sprintf("node-%d", index),
			Database: "db",
			Scripts:  []migration.Script{{Path: fmt.Sprintf("node-%d.sql", index), Checksum: "hash"}},
		})
	}
	createTestRun(t, store, definition)
	at := time.Now().UTC()
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptStarted, RunID: definition.Run.ID, Attempt: 1, GraphFingerprint: "graph-hash", RunStatus: migration.RunStatusRunning, At: at})

	var wait sync.WaitGroup
	errorsCh := make(chan error, len(definition.Graph.Nodes)*4)
	for _, node := range definition.Graph.Nodes {
		node := node
		wait.Add(1)
		go func() {
			defer wait.Done()
			transitions := []execution.Transition{
				{Kind: execution.TransitionNodeStarted, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, NodeStatus: migration.NodeStatusRunning, At: at},
				{Kind: execution.TransitionScriptStarted, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, Script: node.Scripts[0].Path, Checksum: "hash", ScriptStatus: migration.ScriptStatusRunning, At: at},
				{Kind: execution.TransitionScriptFinished, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, Script: node.Scripts[0].Path, ScriptStatus: migration.ScriptStatusSucceeded, At: at},
				{Kind: execution.TransitionNodeFinished, RunID: definition.Run.ID, Attempt: 1, Node: node.Name, NodeStatus: migration.NodeStatusSucceeded, At: at},
				{Kind: execution.TransitionLogAppended, RunID: definition.Run.ID, Attempt: 1, Log: execution.LogEntry{At: at, Level: execution.LogLevelInfo, Node: node.Name, Message: node.Name}},
			}
			for _, transition := range transitions {
				if err := store.Apply(context.Background(), transition); err != nil {
					errorsCh <- err
				}
			}
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent Apply() error = %v", err)
	}

	snapshot, err := store.Snapshot(context.Background(), definition.Run.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Nodes) != 12 || len(snapshot.Logs) != 12 {
		t.Fatalf("concurrent writes were lost: nodes=%d logs=%d", len(snapshot.Nodes), len(snapshot.Logs))
	}
}

func TestStoreConcurrentWritersAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.db")
	first := openTestStore(t, path)
	defer first.Close()
	second := openTestStore(t, path)
	defer second.Close()
	definition := testDefinition("run-two-stores", migration.RunStatusPending, time.Now().UTC())
	createTestRun(t, first, definition)
	at := time.Now().UTC()
	applyTestTransition(t, first, execution.Transition{Kind: execution.TransitionAttemptStarted, RunID: definition.Run.ID, Attempt: 1, GraphFingerprint: "graph-hash", RunStatus: migration.RunStatusRunning, At: at})

	var wait sync.WaitGroup
	errorsCh := make(chan error, 2)
	for index, store := range []*Store{first, second} {
		store := store
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := store.Apply(context.Background(), execution.Transition{
				Kind:    execution.TransitionLogAppended,
				RunID:   definition.Run.ID,
				Attempt: 1,
				Log: execution.LogEntry{
					At:      at.Add(time.Duration(index) * time.Second),
					Level:   execution.LogLevelInfo,
					Message: fmt.Sprintf("store-%d", index),
				},
			})
			errorsCh <- err
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent stores Apply() error = %v", err)
		}
	}

	snapshot, err := first.Snapshot(context.Background(), definition.Run.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Logs) != 2 {
		t.Fatalf("concurrent stores lost logs: %+v", snapshot.Logs)
	}
}

func TestStoreNotFound(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "runs.db"))
	defer store.Close()
	_, err := store.Snapshot(context.Background(), execution.RunID("missing"))
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("Snapshot() error = %v, want ErrRunNotFound", err)
	}
}

func TestStoreConfiguresSQLite(t *testing.T) {
	store := openTestStore(t, ":memory:")
	defer store.Close()

	var foreignKeys, busyTimeout int
	if err := store.db.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys pragma: %v", err)
	}
	if err := store.db.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout pragma: %v", err)
	}
	if foreignKeys != 1 || busyTimeout != busyTimeoutMilliseconds {
		t.Fatalf("unexpected sqlite pragmas: foreign_keys=%d busy_timeout=%d", foreignKeys, busyTimeout)
	}
}

func TestStoreBlockedScriptCanFinishBeforeNode(t *testing.T) {
	store := openTestStore(t, filepath.Join(t.TempDir(), "runs.db"))
	defer store.Close()
	definition := testDefinition("run-blocked", migration.RunStatusPending, time.Now().UTC())
	createTestRun(t, store, definition)
	at := time.Now().UTC()
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionAttemptStarted, RunID: definition.Run.ID, Attempt: 1, GraphFingerprint: "graph-hash", RunStatus: migration.RunStatusRunning, At: at})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionScriptFinished, RunID: definition.Run.ID, Attempt: 1, Node: "report", Script: "report/001.sql", ScriptStatus: migration.ScriptStatusBlocked, Error: "dependency failed", At: at.Add(time.Second)})
	applyTestTransition(t, store, execution.Transition{Kind: execution.TransitionNodeFinished, RunID: definition.Run.ID, Attempt: 1, Node: "report", NodeStatus: migration.NodeStatusBlocked, Error: "dependency failed", At: at.Add(2 * time.Second)})

	snapshot, err := store.Snapshot(context.Background(), definition.Run.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	for _, node := range snapshot.Nodes {
		if node.Name != "report" {
			continue
		}
		if node.Status != migration.NodeStatusBlocked || node.Scripts[0].Status != migration.ScriptStatusBlocked {
			t.Fatalf("blocked projection is wrong: %+v", node)
		}
		return
	}
	t.Fatal("report node missing")
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return store
}

func createTestRun(t *testing.T, store *Store, definition execution.RunDefinition) {
	t.Helper()
	if err := store.Create(context.Background(), definition); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
}

func applyTestTransition(t *testing.T, store *Store, transition execution.Transition) {
	t.Helper()
	if err := store.Apply(context.Background(), transition); err != nil {
		t.Fatalf("Apply(%s) error = %v", transition.Kind, err)
	}
}

func testDefinition(id string, status migration.RunStatus, created time.Time) execution.RunDefinition {
	return execution.RunDefinition{
		Run: execution.Run{
			ID:                     execution.RunID(id),
			GraphName:              "shop",
			GraphFingerprint:       "graph-hash",
			StructureFingerprint:   "structure-hash",
			EnvironmentFingerprint: "environment-hash",
			GraphPath:              "migration.yaml",
			DatabasesPath:          "databases.toml",
			Status:                 status,
		},
		Graph: migration.Graph{
			Name: "shop",
			Nodes: []migration.Node{
				{Name: "user", Database: "user-db", Scripts: []migration.Script{{Path: "user/001.sql", Checksum: "user-hash"}, {Path: "user/002.sql", Checksum: "user-hash-2"}}},
				{Name: "order", Database: "order-db", Scripts: []migration.Script{{Path: "order/001.sql", Checksum: "order-hash"}}},
				{Name: "report", Database: "report-db", DependsOn: []string{"user", "order"}, Scripts: []migration.Script{{Path: "report/001.sql", Checksum: "report-hash"}}},
			},
		},
		Created: created,
	}
}
