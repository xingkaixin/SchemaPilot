package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
)

type FakeConnector struct {
	mu        sync.Mutex
	databases map[string]*FakeTargetDatabase
}

var _ execution.DatabaseConnector = (*FakeConnector)(nil)
var _ execution.TargetDatabase = (*FakeTargetDatabase)(nil)

func NewFakeConnector() *FakeConnector {
	return &FakeConnector{databases: make(map[string]*FakeTargetDatabase)}
}

func (connector *FakeConnector) Connect(_ context.Context, profile migration.DatabaseProfile) (execution.TargetDatabase, error) {
	if connector == nil {
		return nil, errors.New("fake database connector is nil")
	}
	connector.mu.Lock()
	defer connector.mu.Unlock()
	if target, exists := connector.databases[profile.Name]; exists {
		return target, nil
	}
	target := NewFakeTargetDatabase()
	connector.databases[profile.Name] = target
	return target, nil
}

func (connector *FakeConnector) SetDatabase(profileName string, target *FakeTargetDatabase) error {
	if connector == nil {
		return errors.New("fake database connector is nil")
	}
	if profileName == "" {
		return errors.New("fake database profile name is required")
	}
	if target == nil {
		return errors.New("fake database target is nil")
	}
	connector.mu.Lock()
	defer connector.mu.Unlock()
	connector.databases[profileName] = target
	return nil
}

func (connector *FakeConnector) Database(profileName string) *FakeTargetDatabase {
	if connector == nil {
		return nil
	}
	connector.mu.Lock()
	defer connector.mu.Unlock()
	return connector.databases[profileName]
}

type FakeTargetDatabase struct {
	mu sync.Mutex

	applied       map[fakeHistoryKey]FakeAppliedScript
	scriptErrors  map[string]error
	keyLocks      map[fakeHistoryKey]*sync.Mutex
	events        []FakeApplyEvent
	active        int
	maxConcurrent int
	delay         time.Duration
	pingError     error
}

type fakeHistoryKey struct {
	graphName string
	nodeName  string
	path      string
}

type FakeAppliedScript struct {
	Checksum  string
	AppliedAt time.Time
	Forced    bool
}

type FakeApplyEvent struct {
	GraphName string
	NodeName  string
	Path      string
	Checksum  string
	Outcome   execution.ApplyOutcome
	Started   time.Time
	Finished  time.Time
	Error     string
}

func NewFakeTargetDatabase() *FakeTargetDatabase {
	return &FakeTargetDatabase{
		applied:      make(map[fakeHistoryKey]FakeAppliedScript),
		scriptErrors: make(map[string]error),
		keyLocks:     make(map[fakeHistoryKey]*sync.Mutex),
	}
}

func (target *FakeTargetDatabase) Ping(ctx context.Context) error {
	if target == nil {
		return errors.New("fake database target is nil")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	return target.pingError
}

func (target *FakeTargetDatabase) Close() error {
	if target == nil {
		return nil
	}
	return nil
}

func (target *FakeTargetDatabase) Apply(ctx context.Context, application execution.ScriptApplication) (execution.ApplyResult, error) {
	if target == nil {
		return execution.ApplyResult{}, errors.New("fake database target is nil")
	}
	if err := validateApplication(application); err != nil {
		return execution.ApplyResult{}, err
	}
	select {
	case <-ctx.Done():
		return execution.ApplyResult{}, ctx.Err()
	default:
	}
	key := fakeHistoryKey{graphName: application.GraphName, nodeName: application.NodeName, path: application.Path}
	keyLock := target.keyLock(key)
	keyLock.Lock()
	defer keyLock.Unlock()

	target.mu.Lock()
	recorded, found := target.applied[key]
	if found && recorded.Checksum == application.Checksum {
		target.events = append(target.events, FakeApplyEvent{
			GraphName: application.GraphName,
			NodeName:  application.NodeName,
			Path:      application.Path,
			Checksum:  application.Checksum,
			Outcome:   execution.ApplyOutcomeAlreadyApplied,
			Started:   time.Now().UTC(),
			Finished:  time.Now().UTC(),
		})
		target.mu.Unlock()
		return execution.ApplyResult{Outcome: execution.ApplyOutcomeAlreadyApplied}, nil
	}
	if found && !application.Force {
		target.mu.Unlock()
		return execution.ApplyResult{}, &ChecksumMismatchError{Path: application.Path, Recorded: recorded.Checksum, Current: application.Checksum}
	}

	started := time.Now().UTC()
	target.active++
	if target.active > target.maxConcurrent {
		target.maxConcurrent = target.active
	}
	delay := target.delay
	scriptErr := target.scriptErrors[application.Path]
	target.events = append(target.events, FakeApplyEvent{
		GraphName: application.GraphName,
		NodeName:  application.NodeName,
		Path:      application.Path,
		Checksum:  application.Checksum,
		Started:   started,
	})
	eventIndex := len(target.events) - 1
	target.mu.Unlock()

	defer func() {
		target.mu.Lock()
		target.active--
		target.mu.Unlock()
	}()

	if err := waitFakeScript(ctx, delay); err != nil {
		target.finishEvent(eventIndex, err)
		return execution.ApplyResult{}, err
	}
	if scriptErr != nil {
		target.finishEvent(eventIndex, scriptErr)
		return execution.ApplyResult{}, scriptErr
	}

	finished := time.Now().UTC()
	target.mu.Lock()
	target.applied[key] = FakeAppliedScript{Checksum: application.Checksum, AppliedAt: finished, Forced: application.Force}
	target.events[eventIndex].Outcome = execution.ApplyOutcomeApplied
	target.events[eventIndex].Finished = finished
	target.mu.Unlock()
	return execution.ApplyResult{Outcome: execution.ApplyOutcomeApplied}, nil
}

func (target *FakeTargetDatabase) finishEvent(index int, err error) {
	target.mu.Lock()
	defer target.mu.Unlock()
	target.events[index].Finished = time.Now().UTC()
	target.events[index].Error = err.Error()
}

func waitFakeScript(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (target *FakeTargetDatabase) keyLock(key fakeHistoryKey) *sync.Mutex {
	target.mu.Lock()
	defer target.mu.Unlock()
	if lock, exists := target.keyLocks[key]; exists {
		return lock
	}
	lock := &sync.Mutex{}
	target.keyLocks[key] = lock
	return lock
}

func (target *FakeTargetDatabase) SetScriptError(path string, err error) {
	if target == nil {
		return
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	if err == nil {
		delete(target.scriptErrors, path)
		return
	}
	target.scriptErrors[path] = err
}

func (target *FakeTargetDatabase) FailScript(path string, err error) {
	target.SetScriptError(path, err)
}

func (target *FakeTargetDatabase) SetError(path string, err error) {
	target.SetScriptError(path, err)
}

func (target *FakeTargetDatabase) SetDelay(delay time.Duration) {
	if target == nil {
		return
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	target.delay = delay
}

func (target *FakeTargetDatabase) SetPingError(err error) {
	if target == nil {
		return
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	target.pingError = err
}

func (target *FakeTargetDatabase) MaxConcurrency() int {
	if target == nil {
		return 0
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	return target.maxConcurrent
}

func (target *FakeTargetDatabase) MaxConcurrent() int {
	return target.MaxConcurrency()
}

func (target *FakeTargetDatabase) Events() []FakeApplyEvent {
	if target == nil {
		return nil
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	return append([]FakeApplyEvent(nil), target.events...)
}

func (target *FakeTargetDatabase) ApplyEvents() []FakeApplyEvent {
	return target.Events()
}

func (target *FakeTargetDatabase) AppliedScripts() map[string]FakeAppliedScript {
	if target == nil {
		return nil
	}
	target.mu.Lock()
	defer target.mu.Unlock()
	result := make(map[string]FakeAppliedScript, len(target.applied))
	for key, script := range target.applied {
		result[fmt.Sprintf("%s/%s/%s", key.graphName, key.nodeName, key.path)] = script
	}
	return result
}
