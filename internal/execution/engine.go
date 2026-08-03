package execution

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/schemapilot/schemapilot/internal/migration"
)

type Event struct {
	RunID   RunID
	Attempt int
	Log     LogEntry
}

type NotifyFunc func(Event)

type StartRequest struct {
	Project migration.Project
	Force   bool
	Notify  NotifyFunc
}

type ResumeRequest struct {
	RunID   RunID
	Project migration.Project
	Force   bool
	Notify  NotifyFunc
}

type Handle struct {
	ID   RunID
	done <-chan error
}

func (handle Handle) Wait(ctx context.Context) error {
	select {
	case err := <-handle.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type RunFailedError struct {
	RunID RunID
}

func (runError *RunFailedError) Error() string {
	return fmt.Sprintf("migration run %s failed", runError.RunID)
}

type Engine struct {
	store          RunStore
	connector      DatabaseConnector
	activeMu       sync.Mutex
	activeRuns     map[RunID]string
	activeProjects map[string]RunID
}

func NewEngine(store RunStore, connector DatabaseConnector) *Engine {
	return &Engine{
		store:          store,
		connector:      connector,
		activeRuns:     make(map[RunID]string),
		activeProjects: make(map[string]RunID),
	}
}

func (engine *Engine) Start(ctx context.Context, request StartRequest) (Handle, error) {
	if err := validateRunnableProject(request.Project); err != nil {
		return Handle{}, err
	}

	now := time.Now().UTC()
	runID, err := newRunID()
	if err != nil {
		return Handle{}, fmt.Errorf("generate run ID: %w", err)
	}
	run := Run{
		ID:                     runID,
		GraphName:              request.Project.Graph.Name,
		GraphFingerprint:       request.Project.Fingerprint,
		StructureFingerprint:   request.Project.StructureFingerprint,
		EnvironmentFingerprint: request.Project.EnvironmentFingerprint,
		GraphPath:              request.Project.GraphPath,
		DatabasesPath:          request.Project.DatabasesPath,
		Status:                 migration.RunStatusPending,
		StartedAt:              now,
	}
	requestAttempt := executionRequest{
		runID:   runID,
		attempt: 1,
		project: request.Project,
		force:   request.Force,
		notify:  request.Notify,
	}
	if err := engine.reserveAttempt(requestAttempt); err != nil {
		return Handle{}, err
	}
	definition := RunDefinition{Run: run, Graph: request.Project.Graph, Created: now}
	if err := engine.store.Create(ctx, definition); err != nil {
		engine.markInactive(runID)
		return Handle{}, fmt.Errorf("create migration run: %w", err)
	}

	return engine.launchAttempt(ctx, requestAttempt), nil
}

func (engine *Engine) Resume(ctx context.Context, request ResumeRequest) (Handle, error) {
	if err := validateRunnableProject(request.Project); err != nil {
		return Handle{}, err
	}
	snapshot, err := engine.store.Snapshot(ctx, request.RunID)
	if err != nil {
		return Handle{}, fmt.Errorf("load migration run %s: %w", request.RunID, err)
	}
	if snapshot.Run.StructureFingerprint != request.Project.StructureFingerprint {
		return Handle{}, fmt.Errorf("migration graph structure changed since run %s was created", request.RunID)
	}
	if snapshot.Run.EnvironmentFingerprint != request.Project.EnvironmentFingerprint {
		return Handle{}, fmt.Errorf("database profiles changed since run %s was created", request.RunID)
	}
	if snapshot.Run.Status != migration.RunStatusFailed && snapshot.Run.Status != migration.RunStatusCancelled {
		return Handle{}, fmt.Errorf("migration run %s has status %s and cannot be resumed", request.RunID, snapshot.Run.Status)
	}

	attempt := executionRequest{
		runID:   request.RunID,
		attempt: snapshot.Run.Attempt + 1,
		project: request.Project,
		force:   request.Force,
		notify:  request.Notify,
	}
	if err := engine.reserveAttempt(attempt); err != nil {
		return Handle{}, err
	}
	return engine.launchAttempt(ctx, attempt), nil
}

func (engine *Engine) Snapshot(ctx context.Context, runID RunID) (RunSnapshot, error) {
	return engine.store.Snapshot(ctx, runID)
}

func (engine *Engine) Runs(ctx context.Context, query RunQuery) ([]RunSummary, error) {
	return engine.store.Runs(ctx, query)
}

func (engine *Engine) LatestResumable(ctx context.Context) (RunID, error) {
	runs, err := engine.store.Runs(ctx, RunQuery{
		Statuses: []migration.RunStatus{migration.RunStatusFailed, migration.RunStatusCancelled},
		Limit:    1,
	})
	if err != nil {
		return "", err
	}
	if len(runs) == 0 {
		return "", errors.New("no failed or cancelled migration run is available to resume")
	}

	return runs[0].ID, nil
}

func (engine *Engine) launchAttempt(ctx context.Context, request executionRequest) Handle {
	done := make(chan error, 1)
	go func() {
		defer engine.markInactive(request.runID)
		done <- engine.execute(ctx, request)
		close(done)
	}()

	return Handle{ID: request.runID, done: done}
}

func validateRunnableProject(project migration.Project) error {
	if err := migration.ValidateProject(project); err != nil {
		return err
	}
	if project.Fingerprint == "" {
		return errors.New("project fingerprint is required")
	}
	if project.StructureFingerprint == "" {
		return errors.New("project structure fingerprint is required")
	}
	if project.EnvironmentFingerprint == "" {
		return errors.New("project environment fingerprint is required")
	}
	return nil
}

func (engine *Engine) reserveAttempt(request executionRequest) error {
	engine.activeMu.Lock()
	defer engine.activeMu.Unlock()

	if _, exists := engine.activeRuns[request.runID]; exists {
		return fmt.Errorf("migration run %s is already active", request.runID)
	}
	projectKey := request.project.Graph.Name + "\x00" + request.project.EnvironmentFingerprint
	if activeRunID, exists := engine.activeProjects[projectKey]; exists {
		return fmt.Errorf("migration run %s is already active for graph %s and these database profiles", activeRunID, request.project.Graph.Name)
	}
	engine.activeRuns[request.runID] = projectKey
	engine.activeProjects[projectKey] = request.runID
	return nil
}

func (engine *Engine) markInactive(runID RunID) {
	engine.activeMu.Lock()
	defer engine.activeMu.Unlock()
	projectKey, exists := engine.activeRuns[runID]
	if !exists {
		return
	}
	delete(engine.activeRuns, runID)
	delete(engine.activeProjects, projectKey)
}

func newRunID() (RunID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80

	return RunID(fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	)), nil
}
