package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/sqlscript"
	"github.com/schemapilot/schemapilot/internal/workspace"
)

type Status string

const (
	Pending   Status = "pending"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

const maxLogEntries = 500

var ErrBusy = errors.New("该连接正在运行")

// Plan lists files per step, then per parallel lane: steps run one after
// another, lanes of a step run side by side, files of a lane run in order.
type Plan struct {
	Connection string       `json:"connection"`
	Steps      [][][]string `json:"steps"`
}

type StatementError struct {
	Index     int `json:"index"`
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
	Line      int `json:"line,omitempty"`
	database.ErrorInfo
}

type LogEntry struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	Index      int       `json:"index,omitempty"`
	Text       string    `json:"text"`
	Rows       *int64    `json:"rows,omitempty"`
	DurationMs int64     `json:"durationMs,omitempty"`
}

type FileRun struct {
	Path             string          `json:"path"`
	Status           Status          `json:"status"`
	StartedAt        *time.Time      `json:"startedAt,omitempty"`
	FinishedAt       *time.Time      `json:"finishedAt,omitempty"`
	Statements       int             `json:"statements"`
	Executed         int             `json:"executed"`
	Current          int             `json:"current,omitempty"`
	CurrentStartedAt *time.Time      `json:"currentStartedAt,omitempty"`
	CurrentText      string          `json:"currentText,omitempty"`
	RowsAffected     int64           `json:"rowsAffected"`
	Session          int64           `json:"session,omitempty"`
	Error            *StatementError `json:"error,omitempty"`
	Message          string          `json:"message,omitempty"`
	Log              []LogEntry      `json:"log"`
	LogDropped       int             `json:"logDropped,omitempty"`
}

type Run struct {
	ID         string     `json:"id"`
	Connection string     `json:"connection"`
	Status     Status     `json:"status"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
	// Version is the server version reported when the run connected.
	Version string     `json:"version,omitempty"`
	Files   []*FileRun `json:"files"`
}

type Manager struct {
	ctx       context.Context
	workspace workspace.Workspace

	mu   sync.Mutex
	runs map[string]*execution
}

func NewManager(ctx context.Context, workspace workspace.Workspace) *Manager {
	return &Manager{ctx: ctx, workspace: workspace, runs: map[string]*execution{}}
}

type execution struct {
	mu       sync.Mutex
	run      Run
	files    map[string]*FileRun
	cancel   context.CancelFunc
	failed   bool
	stopping bool
	db       *database.DB
	sessions map[int64]struct{}
	done     chan struct{}
}

func (manager *Manager) Start(plan Plan, connection config.Connection) (Run, error) {
	if err := validate(plan); err != nil {
		return Run{}, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if existing, ok := manager.runs[plan.Connection]; ok && existing.snapshot().Status == Running {
		return Run{}, ErrBusy
	}
	ctx, cancel := context.WithCancel(manager.ctx)
	run := &execution{
		run: Run{
			ID:         newID(),
			Connection: plan.Connection,
			Status:     Running,
			StartedAt:  time.Now().UTC(),
		},
		files:    map[string]*FileRun{},
		cancel:   cancel,
		sessions: map[int64]struct{}{},
		done:     make(chan struct{}),
	}
	for _, step := range plan.Steps {
		for _, lane := range step {
			for _, path := range lane {
				file := &FileRun{Path: path, Status: Pending, Log: []LogEntry{}}
				run.files[path] = file
				run.run.Files = append(run.run.Files, file)
			}
		}
	}
	manager.runs[plan.Connection] = run
	go run.execute(ctx, manager.workspace, plan, connection)
	return run.snapshot(), nil
}

func (manager *Manager) Get(connection string) (Run, bool) {
	manager.mu.Lock()
	run, ok := manager.runs[connection]
	manager.mu.Unlock()
	if !ok {
		return Run{}, false
	}
	return run.snapshot(), true
}

func (manager *Manager) Stop(connection string) error {
	manager.mu.Lock()
	run, ok := manager.runs[connection]
	manager.mu.Unlock()
	if !ok {
		return errors.New("没有正在运行的任务")
	}
	run.stop()
	return nil
}

// Wait blocks until the connection's current run finishes.
func (manager *Manager) Wait(connection string) {
	manager.mu.Lock()
	run, ok := manager.runs[connection]
	manager.mu.Unlock()
	if ok {
		<-run.done
	}
}

func validate(plan Plan) error {
	if plan.Connection == "" {
		return errors.New("缺少连接")
	}
	seen := map[string]bool{}
	for _, step := range plan.Steps {
		for _, lane := range step {
			for _, path := range lane {
				if seen[path] {
					return fmt.Errorf("文件 %s 在编排中重复出现", path)
				}
				seen[path] = true
			}
		}
	}
	if len(seen) == 0 {
		return errors.New("没有需要执行的文件")
	}
	return nil
}

func (run *execution) execute(ctx context.Context, files workspace.Workspace, plan Plan, connection config.Connection) {
	defer close(run.done)
	defer run.cancel()

	db, err := database.Open(ctx, connection)
	if err != nil {
		run.finish(err.Error())
		return
	}
	defer db.Close()
	version, _ := db.Version(ctx)
	run.mu.Lock()
	run.db = db
	run.run.Version = version
	run.mu.Unlock()

	for _, step := range plan.Steps {
		if run.halted(ctx) {
			break
		}
		var lanes sync.WaitGroup
		for _, lane := range step {
			lanes.Add(1)
			go func() {
				defer lanes.Done()
				run.executeLane(ctx, files, db, lane)
			}()
		}
		lanes.Wait()
	}
	run.finish("")
}

func (run *execution) halted(ctx context.Context) bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.failed || run.stopping || ctx.Err() != nil
}

func (run *execution) executeLane(ctx context.Context, files workspace.Workspace, db *database.DB, lane []string) {
	if len(lane) == 0 || run.halted(ctx) {
		return
	}
	session, err := db.Session(ctx)
	if err != nil {
		run.update(lane[0], func(file *FileRun) {
			file.Status = Failed
			file.Message = err.Error()
		})
		run.markFailed()
		return
	}
	defer session.Close()
	run.mu.Lock()
	run.sessions[session.ID] = struct{}{}
	run.mu.Unlock()
	defer func() {
		run.mu.Lock()
		delete(run.sessions, session.ID)
		run.mu.Unlock()
	}()

	var current string
	unsubscribe := session.OnNotice(func(message string) {
		run.update(current, func(file *FileRun) {
			file.appendLog(LogEntry{At: time.Now().UTC(), Kind: "notice", Index: file.Current, Text: message})
		})
	})
	defer unsubscribe()

	for _, path := range lane {
		if run.halted(ctx) {
			return
		}
		current = path
		if status := run.executeFile(ctx, files, db, session, path); status != Succeeded {
			if status == Failed {
				run.markFailed()
			}
			return
		}
	}
}

func (run *execution) executeFile(ctx context.Context, files workspace.Workspace, db *database.DB, session *database.Session, path string) Status {
	startedAt := time.Now().UTC()
	run.update(path, func(file *FileRun) {
		file.Status = Running
		file.StartedAt = &startedAt
		file.Session = session.ID
	})
	content, err := files.Read(path)
	if err != nil {
		return run.end(path, Failed, func(file *FileRun) { file.Message = err.Error() })
	}
	statements := sqlscript.Split(string(content), db.Dialect())
	run.update(path, func(file *FileRun) { file.Statements = len(statements) })

	for _, statement := range statements {
		if ctx.Err() != nil {
			return run.end(path, Cancelled, nil)
		}
		statementStart := time.Now().UTC()
		run.update(path, func(file *FileRun) {
			file.Current = statement.Index
			file.CurrentStartedAt = &statementStart
			file.CurrentText = summarize(statement.Text)
		})
		rows, err := session.Exec(ctx, statement.Text)
		duration := time.Since(statementStart).Milliseconds()
		if err != nil {
			if run.isStopping() || ctx.Err() != nil {
				return run.end(path, Cancelled, func(file *FileRun) {
					file.Error = &StatementError{
						Index:     statement.Index,
						StartLine: statement.StartLine,
						EndLine:   statement.EndLine,
						ErrorInfo: database.ErrorInfo{Message: "已手动终止"},
					}
				})
			}
			info := db.Describe(err)
			return run.end(path, Failed, func(file *FileRun) {
				file.Error = &StatementError{
					Index:     statement.Index,
					StartLine: statement.StartLine,
					EndLine:   statement.EndLine,
					Line:      errorLine(statement, info),
					ErrorInfo: info,
				}
				file.appendLog(LogEntry{At: statementStart, Kind: "error", Index: statement.Index, Text: summarize(statement.Text), DurationMs: duration})
			})
		}
		run.update(path, func(file *FileRun) {
			file.Executed++
			entry := LogEntry{At: statementStart, Kind: "statement", Index: statement.Index, Text: summarize(statement.Text), DurationMs: duration}
			if rows >= 0 {
				file.RowsAffected += rows
				entry.Rows = &rows
			}
			file.appendLog(entry)
		})
	}
	return run.end(path, Succeeded, nil)
}

func (run *execution) end(path string, status Status, change func(*FileRun)) Status {
	finishedAt := time.Now().UTC()
	run.update(path, func(file *FileRun) {
		file.Status = status
		file.FinishedAt = &finishedAt
		file.Current = 0
		file.CurrentStartedAt = nil
		file.CurrentText = ""
		if change != nil {
			change(file)
		}
	})
	return status
}

func (run *execution) update(path string, change func(*FileRun)) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if file, ok := run.files[path]; ok {
		change(file)
	}
}

func (run *execution) markFailed() {
	run.mu.Lock()
	run.failed = true
	run.mu.Unlock()
}

func (run *execution) isStopping() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.stopping
}

// stop asks the database to cancel every running statement before the
// context is cancelled; closing a MySQL connection alone does not stop
// the query on the server.
func (run *execution) stop() {
	run.mu.Lock()
	if run.run.Status != Running {
		run.mu.Unlock()
		return
	}
	run.stopping = true
	db := run.db
	sessions := make([]int64, 0, len(run.sessions))
	for session := range run.sessions {
		sessions = append(sessions, session)
	}
	run.mu.Unlock()

	if db != nil {
		var cancels sync.WaitGroup
		for _, session := range sessions {
			cancels.Add(1)
			go func() {
				defer cancels.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = db.Cancel(ctx, session)
			}()
		}
		cancels.Wait()
	}
	run.cancel()
}

func (run *execution) finish(message string) {
	run.mu.Lock()
	defer run.mu.Unlock()
	finishedAt := time.Now().UTC()
	run.run.FinishedAt = &finishedAt
	run.run.Error = message
	switch {
	case run.stopping:
		run.run.Status = Cancelled
	case run.failed || message != "":
		run.run.Status = Failed
	default:
		run.run.Status = Succeeded
	}
}

func (run *execution) snapshot() Run {
	run.mu.Lock()
	defer run.mu.Unlock()
	copied := run.run
	copied.Files = make([]*FileRun, len(run.run.Files))
	for index, file := range run.run.Files {
		clone := *file
		clone.Log = append([]LogEntry(nil), file.Log...)
		copied.Files[index] = &clone
	}
	return copied
}

func (file *FileRun) appendLog(entry LogEntry) {
	file.Log = append(file.Log, entry)
	if overflow := len(file.Log) - maxLogEntries; overflow > 0 {
		file.Log = append([]LogEntry(nil), file.Log[overflow:]...)
		file.LogDropped += overflow
	}
}

func summarize(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(collapsed) <= 200 {
		return collapsed
	}
	return string([]rune(collapsed)[:200]) + "…"
}

func errorLine(statement sqlscript.Statement, info database.ErrorInfo) int {
	if info.StatementLine > 0 {
		return statement.StartLine + info.StatementLine - 1
	}
	return lineAt(statement, info.Position)
}

// lineAt converts a server-reported 1-based character position inside the
// statement into a line number of the file.
func lineAt(statement sqlscript.Statement, position int) int {
	if position <= 0 {
		return 0
	}
	line := statement.StartLine
	for index, char := range []rune(statement.Text) {
		if index >= position-1 {
			break
		}
		if char == '\n' {
			line++
		}
	}
	return line
}

func newID() string {
	buffer := make([]byte, 8)
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
