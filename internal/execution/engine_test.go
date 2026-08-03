package execution_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
	"github.com/schemapilot/schemapilot/internal/runstore"
)

func TestEngineDAGRunsIndependentNodesInParallel(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	target.SetDelay(30 * time.Millisecond)
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("dag", 2, migration.ErrorPolicyHalt,
		migration.Node{Name: "alpha", Database: "primary", Scripts: []migration.Script{testScript("alpha.sql", "alpha-v1")}},
		migration.Node{Name: "beta", Database: "primary", Scripts: []migration.Script{testScript("beta.sql", "beta-v1")}},
		migration.Node{Name: "report", Database: "primary", DependsOn: []string{"alpha", "beta"}, Scripts: []migration.Script{testScript("report.sql", "report-v1")}},
	)
	engine, _ := testEngine(t, connector)
	handle, err := engine.Start(context.Background(), execution.StartRequest{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Snapshot(context.Background(), handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run.Status != migration.RunStatusSucceeded {
		t.Fatalf("run status = %s", snapshot.Run.Status)
	}
	if target.MaxConcurrency() < 2 {
		t.Fatalf("independent nodes did not run concurrently: max=%d", target.MaxConcurrency())
	}
	events := eventsByPath(target.Events())
	for _, dependency := range []string{"alpha.sql", "beta.sql"} {
		if events[dependency].Finished.IsZero() {
			t.Fatalf("dependency %s did not finish: %+v", dependency, events[dependency])
		}
	}
	if events["report.sql"].Started.Before(events["alpha.sql"].Finished) || events["report.sql"].Started.Before(events["beta.sql"].Finished) {
		t.Fatalf("report started before dependencies finished: %+v", events)
	}
}

func TestEngineBlocksDependentsAfterFailure(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	target.SetScriptError("alpha.sql", errors.New("alpha failed"))
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("blocked", 2, migration.ErrorPolicyHalt,
		migration.Node{Name: "alpha", Database: "primary", Scripts: []migration.Script{testScript("alpha.sql", "alpha-v1")}},
		migration.Node{Name: "report", Database: "primary", DependsOn: []string{"alpha"}, Scripts: []migration.Script{testScript("report.sql", "report-v1")}},
	)
	engine, _ := testEngine(t, connector)
	handle, err := engine.Start(context.Background(), execution.StartRequest{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); err == nil {
		t.Fatal("failed run returned nil error")
	}
	snapshot, err := engine.Snapshot(context.Background(), handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run.Status != migration.RunStatusFailed {
		t.Fatalf("run status = %s", snapshot.Run.Status)
	}
	nodes := nodeSnapshots(snapshot)
	if nodes["alpha"].Status != migration.NodeStatusFailed || nodes["report"].Status != migration.NodeStatusBlocked {
		t.Fatalf("node statuses = %+v", nodes)
	}
	if _, exists := eventsByPath(target.Events())["report.sql"]; exists {
		t.Fatal("blocked dependent was applied")
	}
}

func TestEngineContinueCompletesWithErrorsAndRunsDependents(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	target.SetScriptError("alpha.sql", errors.New("alpha failed"))
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("continue", 2, migration.ErrorPolicyContinue,
		migration.Node{Name: "alpha", Database: "primary", Scripts: []migration.Script{testScript("alpha.sql", "alpha-v1")}},
		migration.Node{Name: "report", Database: "primary", DependsOn: []string{"alpha"}, Scripts: []migration.Script{testScript("report.sql", "report-v1")}},
	)
	engine, _ := testEngine(t, connector)
	handle, err := engine.Start(context.Background(), execution.StartRequest{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Snapshot(context.Background(), handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run.Status != migration.RunStatusCompletedWithErrors {
		t.Fatalf("run status = %s", snapshot.Run.Status)
	}
	nodes := nodeSnapshots(snapshot)
	if nodes["alpha"].Status != migration.NodeStatusCompletedWithErrors || nodes["report"].Status != migration.NodeStatusSucceeded {
		t.Fatalf("node statuses = %+v", nodes)
	}
	if _, exists := eventsByPath(target.Events())["report.sql"]; !exists {
		t.Fatal("dependent did not run after continue policy")
	}
}

func TestEngineResumeAfterChangingFailedScript(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	target.SetScriptError("second.sql", errors.New("second failed"))
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("resume", 1, migration.ErrorPolicyHalt,
		migration.Node{Name: "users", Database: "primary", Scripts: []migration.Script{
			testScript("first.sql", "first-v1"),
			testScript("second.sql", "second-v1"),
		}},
	)
	engine, _ := testEngine(t, connector)
	firstHandle, err := engine.Start(context.Background(), execution.StartRequest{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := firstHandle.Wait(context.Background()); err == nil {
		t.Fatal("initial failed run returned nil error")
	}
	firstSnapshot, err := engine.Snapshot(context.Background(), firstHandle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstSnapshot.Run.Status != migration.RunStatusFailed || nodeSnapshots(firstSnapshot)["users"].Status != migration.NodeStatusFailed {
		t.Fatalf("initial snapshot = %+v", firstSnapshot)
	}
	target.SetScriptError("second.sql", nil)
	project.Graph.Nodes[0].Scripts[1].Checksum = "second-v2"
	project.Graph.Nodes[0].Scripts[1].SQL = "alter table users add column name text;"
	project.Fingerprint = "resume-graph-v2"
	secondHandle, err := engine.Resume(context.Background(), execution.ResumeRequest{RunID: firstHandle.ID, Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := secondHandle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.Snapshot(context.Background(), firstHandle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run.Status != migration.RunStatusSucceeded || len(snapshot.Attempts) != 2 {
		t.Fatalf("resumed snapshot = %+v", snapshot)
	}
	users := nodeSnapshots(snapshot)["users"]
	if len(users.Scripts) != 2 || users.Scripts[0].Status != migration.ScriptStatusAlreadyApplied || users.Scripts[0].Attempt != 2 || users.Scripts[1].Status != migration.ScriptStatusSucceeded || users.Scripts[1].Checksum != "second-v2" || users.Scripts[1].Attempt != 2 {
		t.Fatalf("resumed scripts = %+v", users.Scripts)
	}
}

func TestEngineChecksumMismatchHaltsEvenWhenContinue(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("mismatch", 1, migration.ErrorPolicyContinue,
		migration.Node{Name: "users", Database: "primary", Scripts: []migration.Script{
			testScript("users.sql", "new-checksum"),
			testScript("report.sql", "report-v1"),
		}},
	)
	_, err := target.Apply(context.Background(), execution.ScriptApplication{GraphName: project.Graph.Name, NodeName: "users", Path: "users.sql", Checksum: "old-checksum", SQL: "create table users (id bigint);"})
	if err != nil {
		t.Fatal(err)
	}
	engine, _ := testEngine(t, connector)
	handle, err := engine.Start(context.Background(), execution.StartRequest{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); err == nil {
		t.Fatal("checksum mismatch returned nil error")
	}
	snapshot, err := engine.Snapshot(context.Background(), handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	users := nodeSnapshots(snapshot)["users"]
	if snapshot.Run.Status != migration.RunStatusFailed || users.Status != migration.NodeStatusFailed || users.Scripts[0].Status != migration.ScriptStatusFailed || users.Scripts[1].Status != migration.ScriptStatusBlocked {
		t.Fatalf("mismatch snapshot = %+v", snapshot)
	}
	if _, exists := eventsByPath(target.Events())["report.sql"]; exists {
		t.Fatal("script after checksum mismatch was applied")
	}
}

func TestEngineForceOverwritesChecksumMismatch(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("force", 1, migration.ErrorPolicyHalt,
		migration.Node{Name: "users", Database: "primary", Scripts: []migration.Script{testScript("users.sql", "new-checksum")}},
	)
	if _, err := target.Apply(context.Background(), execution.ScriptApplication{GraphName: project.Graph.Name, NodeName: "users", Path: "users.sql", Checksum: "old-checksum", SQL: "create table users (id bigint);"}); err != nil {
		t.Fatal(err)
	}
	engine, _ := testEngine(t, connector)
	handle, err := engine.Start(context.Background(), execution.StartRequest{Project: project, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := target.AppliedScripts()["force/users/users.sql"]; got.Checksum != "new-checksum" || !got.Forced {
		t.Fatalf("forced script record = %+v", got)
	}
}

func TestEngineCancellationRecordsCancelledState(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	target.SetDelay(500 * time.Millisecond)
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("cancel", 1, migration.ErrorPolicyHalt,
		migration.Node{Name: "users", Database: "primary", Scripts: []migration.Script{testScript("users.sql", "users-v1")}},
	)
	engine, _ := testEngine(t, connector)
	ctx, cancel := context.WithCancel(context.Background())
	handle, err := engine.Start(ctx, execution.StartRequest{Project: project})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		events := target.Events()
		return len(events) > 0 && events[0].Finished.IsZero()
	})
	cancel()
	waitErr := handle.Wait(context.Background())
	if !errors.Is(waitErr, context.Canceled) {
		snapshot, snapshotErr := engine.Snapshot(context.Background(), handle.ID)
		t.Fatalf("wait error = %v, snapshot status = %s, snapshot error = %v", waitErr, snapshot.Run.Status, snapshotErr)
	}
	snapshot, err := engine.Snapshot(context.Background(), handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	users := nodeSnapshots(snapshot)["users"]
	if snapshot.Run.Status != migration.RunStatusCancelled || users.Status != migration.NodeStatusCancelled || users.Scripts[0].Status != migration.ScriptStatusCancelled {
		t.Fatalf("cancelled snapshot = %+v", snapshot)
	}
}

func TestEngineDeduplicatesActiveGraphAndEnvironment(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	target.SetDelay(100 * time.Millisecond)
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("dedupe", 1, migration.ErrorPolicyHalt,
		migration.Node{Name: "users", Database: "primary", Scripts: []migration.Script{testScript("users.sql", "users-v1")}},
	)
	engine, _ := testEngine(t, connector)
	handle, err := engine.Start(context.Background(), execution.StartRequest{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Start(context.Background(), execution.StartRequest{Project: project}); err == nil {
		t.Fatal("duplicate active project was accepted")
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := engine.Start(context.Background(), execution.StartRequest{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEngineNotifiesSerially(t *testing.T) {
	target := database.NewFakeTargetDatabase()
	target.SetDelay(10 * time.Millisecond)
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		t.Fatal(err)
	}
	project := testProject("notify", 3, migration.ErrorPolicyHalt,
		migration.Node{Name: "alpha", Database: "primary", Scripts: []migration.Script{testScript("alpha.sql", "alpha-v1")}},
		migration.Node{Name: "beta", Database: "primary", Scripts: []migration.Script{testScript("beta.sql", "beta-v1")}},
		migration.Node{Name: "gamma", Database: "primary", Scripts: []migration.Script{testScript("gamma.sql", "gamma-v1")}},
	)
	var current atomic.Int32
	var maximum atomic.Int32
	var eventsMu sync.Mutex
	events := make([]execution.Event, 0)
	notify := func(event execution.Event) {
		active := current.Add(1)
		for {
			old := maximum.Load()
			if active <= old || maximum.CompareAndSwap(old, active) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
		current.Add(-1)
	}
	engine, _ := testEngine(t, connector)
	handle, err := engine.Start(context.Background(), execution.StartRequest{Project: project, Notify: notify})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 1 {
		t.Fatalf("notify callback ran concurrently: max=%d", maximum.Load())
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) == 0 {
		t.Fatal("notify callback was never invoked")
	}
}

func testEngine(t *testing.T, connector execution.DatabaseConnector) (*execution.Engine, *runstore.Store) {
	t.Helper()
	store, err := runstore.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return execution.NewEngine(store, connector), store
}

func testProject(name string, parallelism int, policy migration.ErrorPolicy, nodes ...migration.Node) migration.Project {
	databases := make(map[string]migration.DatabaseProfile)
	for _, node := range nodes {
		if _, exists := databases[node.Database]; exists {
			continue
		}
		databases[node.Database] = migration.DatabaseProfile{Name: node.Database, Driver: migration.DriverPostgres, DSN: "fake://" + node.Database, MaxOpenConnections: 1}
	}
	return migration.Project{
		Graph:                  migration.Graph{Version: migration.CurrentGraphVersion, Name: name, Parallelism: parallelism, OnError: policy, Nodes: nodes},
		Databases:              databases,
		Fingerprint:            "fingerprint-" + name,
		StructureFingerprint:   "structure-" + name,
		EnvironmentFingerprint: "environment-" + name,
	}
}

func testScript(path, checksum string) migration.Script {
	return migration.Script{Path: path, Checksum: checksum, SQL: "select 1;"}
}

func nodeSnapshots(snapshot execution.RunSnapshot) map[string]execution.NodeSnapshot {
	result := make(map[string]execution.NodeSnapshot, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		result[node.Name] = node
	}
	return result
}

func eventsByPath(events []database.FakeApplyEvent) map[string]database.FakeApplyEvent {
	result := make(map[string]database.FakeApplyEvent, len(events))
	for _, event := range events {
		result[event.Path] = event
	}
	return result
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not reached before timeout")
}
