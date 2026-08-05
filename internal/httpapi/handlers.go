package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
	"github.com/schemapilot/schemapilot/internal/project"
)

const maxRunListLimit = 200

func (server *Server) getProject(response http.ResponseWriter, request *http.Request) {
	workspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	server.writeWorkspace(response, http.StatusOK, workspace)
}

func (server *Server) putGraph(response http.ResponseWriter, request *http.Request) {
	server.workspaceMu.Lock()
	defer server.workspaceMu.Unlock()

	current, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !matchesETag(request.Header.Get("If-Match"), current.Fingerprint) {
		writeError(response, http.StatusPreconditionFailed, "migration graph changed since it was loaded")
		return
	}

	var graph graphDTO
	if err := readJSON(response, request, &graph); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	updatedGraph := graph.domain()
	if err := validateGraphDatabases(updatedGraph, current.Databases); err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := project.SaveGraph(request.Context(), server.graphPath, updatedGraph); err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}

	workspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	server.writeWorkspace(response, http.StatusOK, workspace)
}

func validateGraphDatabases(graph migration.Graph, databases map[string]migration.DatabaseProfile) error {
	for _, node := range graph.Nodes {
		if _, exists := databases[node.Database]; !exists {
			return fmt.Errorf("node %q references unknown database profile %q", node.Name, node.Database)
		}
	}
	return nil
}

func (server *Server) getScript(response http.ResponseWriter, request *http.Request) {
	workspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	path, err := resolveSQLPath(workspace.RootDirectory, request.URL.Query().Get("path"))
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	content, err := readSQLFile(path.absolute)
	if err != nil {
		writeError(response, http.StatusNotFound, err.Error())
		return
	}
	checksum := checksum(content)
	response.Header().Set("ETag", quotedETag(checksum))
	writeJSON(response, http.StatusOK, scriptResponse{
		Path:        path.relative,
		Content:     string(content),
		Checksum:    checksum,
		Fingerprint: workspace.Fingerprint,
	})
}

func (server *Server) postScript(response http.ResponseWriter, request *http.Request) {
	server.workspaceMu.Lock()
	defer server.workspaceMu.Unlock()

	workspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var create scriptCreateRequest
	if err := readJSON(response, request, &create); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	path, err := createSQLFile(request.Context(), workspace.RootDirectory, create.Path, []byte(create.Content))
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			writeError(response, http.StatusConflict, err.Error())
			return
		}
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	fileChecksum := checksum([]byte(create.Content))
	response.Header().Set("ETag", quotedETag(fileChecksum))
	writeJSON(response, http.StatusCreated, scriptResponse{
		Path:        path.relative,
		Content:     create.Content,
		Checksum:    fileChecksum,
		Fingerprint: updated.Fingerprint,
	})
}

func (server *Server) putScript(response http.ResponseWriter, request *http.Request) {
	server.workspaceMu.Lock()
	defer server.workspaceMu.Unlock()

	workspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	path, err := resolveSQLPath(workspace.RootDirectory, request.URL.Query().Get("path"))
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	current, err := readSQLFile(path.absolute)
	if err != nil {
		writeError(response, http.StatusNotFound, err.Error())
		return
	}
	if !matchesETag(request.Header.Get("If-Match"), checksum(current)) {
		writeError(response, http.StatusPreconditionFailed, "SQL file changed since it was loaded")
		return
	}

	var update scriptUpdateRequest
	if err := readJSON(response, request, &update); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	if err := writeSQLFile(request.Context(), path.absolute, []byte(update.Content)); err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	updatedWorkspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	updatedChecksum := checksum([]byte(update.Content))
	response.Header().Set("ETag", quotedETag(updatedChecksum))
	writeJSON(response, http.StatusOK, scriptResponse{
		Path:        path.relative,
		Content:     update.Content,
		Checksum:    updatedChecksum,
		Fingerprint: updatedWorkspace.Fingerprint,
	})
}

func (server *Server) putDatabase(response http.ResponseWriter, request *http.Request) {
	server.workspaceMu.Lock()
	defer server.workspaceMu.Unlock()

	current, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !matchesETag(request.Header.Get("If-Match"), current.DatabaseFingerprint) {
		writeError(response, http.StatusPreconditionFailed, "database profiles changed since they were loaded")
		return
	}
	var update databaseUpdateRequest
	if err := readJSON(response, request, &update); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	timeout, err := time.ParseDuration(update.ConnectionTimeout)
	if err != nil {
		writeError(response, http.StatusBadRequest, "connection_timeout must be a duration")
		return
	}
	if err := project.SaveDatabaseProfile(request.Context(), server.databasesPath, request.PathValue("name"), project.DatabaseProfileInput{
		Driver:             update.Driver,
		DSN:                update.DSN,
		MaxOpenConnections: update.MaxOpenConnections,
		ConnectionTimeout:  timeout,
	}); err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	workspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	server.writeDatabaseWorkspace(response, http.StatusOK, workspace)
}

func (server *Server) deleteDatabase(response http.ResponseWriter, request *http.Request) {
	server.workspaceMu.Lock()
	defer server.workspaceMu.Unlock()

	current, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !matchesETag(request.Header.Get("If-Match"), current.DatabaseFingerprint) {
		writeError(response, http.StatusPreconditionFailed, "database profiles changed since they were loaded")
		return
	}
	if err := project.DeleteDatabaseProfile(request.Context(), server.databasesPath, request.PathValue("name")); err != nil {
		if errors.Is(err, project.ErrDatabaseProfileNotFound) {
			writeError(response, http.StatusNotFound, err.Error())
			return
		}
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	workspace, err := server.loadWorkspace(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	server.writeDatabaseWorkspace(response, http.StatusOK, workspace)
}

func (server *Server) testDatabase(response http.ResponseWriter, request *http.Request) {
	databases, err := project.LoadDatabases(request.Context(), server.databasesPath)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	name := request.PathValue("name")
	profile, exists := databases[name]
	if !exists {
		writeError(response, http.StatusNotFound, fmt.Sprintf("database profile %q does not exist", name))
		return
	}

	startedAt := time.Now()
	target, err := server.connector.Connect(request.Context(), profile)
	if err != nil {
		writeError(response, http.StatusBadGateway, err.Error())
		return
	}
	defer target.Close()
	if err := target.Ping(request.Context()); err != nil {
		writeError(response, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, databaseTestResponse{Name: name, LatencyMS: time.Since(startedAt).Milliseconds()})
}

func (server *Server) getRuns(response http.ResponseWriter, request *http.Request) {
	limit, err := runListLimit(request.URL.Query().Get("limit"))
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	runs, err := server.engine.Runs(request.Context(), execution.RunQuery{Limit: limit})
	if err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]runSummaryDTO, 0, len(runs))
	for _, run := range runs {
		items = append(items, summaryDTO(run))
	}
	writeJSON(response, http.StatusOK, runsResponse{Runs: items})
}

func (server *Server) startRun(response http.ResponseWriter, request *http.Request) {
	var start startRunRequest
	if err := readOptionalJSON(response, request, &start); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	loaded, err := server.loadProject(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	scoped, err := project.Scoped(loaded, start.Nodes)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	handle, err := server.engine.Start(server.context, execution.StartRequest{Project: scoped, Force: start.Force})
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	writeJSON(response, http.StatusAccepted, runAcceptedResponse{ID: handle.ID, Status: migration.RunStatusPending})
}

func (server *Server) getRun(response http.ResponseWriter, request *http.Request) {
	snapshot, err := server.engine.Snapshot(request.Context(), execution.RunID(request.PathValue("id")))
	if err != nil {
		writeRunError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, snapshotDTO(snapshot))
}

func (server *Server) resumeRun(response http.ResponseWriter, request *http.Request) {
	var resume startRunRequest
	if err := readOptionalJSON(response, request, &resume); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	loaded, err := server.loadProject(request)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	runID := execution.RunID(request.PathValue("id"))
	snapshot, err := server.engine.Snapshot(request.Context(), runID)
	if err != nil {
		writeRunError(response, err)
		return
	}
	// A scoped run resumes against the same subgraph it was created with.
	names := make([]string, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		names = append(names, node.Name)
	}
	scoped, err := project.Scoped(loaded, names)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, err.Error())
		return
	}
	handle, err := server.engine.Resume(server.context, execution.ResumeRequest{RunID: runID, Project: scoped, Force: resume.Force})
	if err != nil {
		writeRunError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, runAcceptedResponse{ID: handle.ID, Status: migration.RunStatusPending})
}

func (server *Server) loadProject(request *http.Request) (migration.Project, error) {
	return project.Load(request.Context(), server.graphPath, server.databasesPath)
}

func (server *Server) loadWorkspace(request *http.Request) (project.Workspace, error) {
	return project.Inspect(request.Context(), server.graphPath, server.databasesPath)
}

func (server *Server) writeWorkspace(response http.ResponseWriter, status int, workspace project.Workspace) {
	response.Header().Set("ETag", quotedETag(workspace.Fingerprint))
	writeJSON(response, status, projectDTO(workspace))
}

func (server *Server) writeDatabaseWorkspace(response http.ResponseWriter, status int, workspace project.Workspace) {
	response.Header().Set("ETag", quotedETag(workspace.DatabaseFingerprint))
	writeJSON(response, status, projectDTO(workspace))
}

func writeRunError(response http.ResponseWriter, err error) {
	if errors.Is(err, execution.ErrRunNotFound) {
		writeError(response, http.StatusNotFound, err.Error())
		return
	}
	writeError(response, http.StatusConflict, err.Error())
}

func runListLimit(raw string) (int, error) {
	if raw == "" {
		return 50, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxRunListLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxRunListLimit)
	}
	return limit, nil
}

func quotedETag(value string) string {
	return `"` + value + `"`
}

func matchesETag(header string, current string) bool {
	return header == "*" || strings.TrimSpace(header) == quotedETag(current)
}

func checksum(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
