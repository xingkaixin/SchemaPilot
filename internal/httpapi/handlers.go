package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/schemapilot/schemapilot/internal/arrangement"
	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/runner"
	"github.com/schemapilot/schemapilot/internal/sqlscript"
	"github.com/schemapilot/schemapilot/internal/workspace"
)

const (
	maxCountedFileBytes = 8 << 20
	maxViewedFileBytes  = 2 << 20
	maxImportBytes      = 256 << 20
)

type workspaceFile struct {
	workspace.File
	Statements *int `json:"statements"`
}

type workspaceResponse struct {
	Root         string              `json:"root"`
	ConfigFile   string              `json:"configFile"`
	ConfigExists bool                `json:"configExists"`
	ConfigError  string              `json:"configError,omitempty"`
	Drivers      []database.Info     `json:"drivers"`
	Connections  []config.Connection `json:"connections"`
	Files        []workspaceFile     `json:"files"`

	Arrangement         *arrangement.Arrangement `json:"arrangement"`
	ArrangementFile     string                   `json:"arrangementFile"`
	ArrangementRevision string                   `json:"arrangementRevision"`
	ArrangementError    string                   `json:"arrangementError,omitempty"`
}

func (server *Server) getWorkspace(response http.ResponseWriter, _ *http.Request) {
	loaded, exists, loadErr := config.Load(server.workspace.Root)
	result := workspaceResponse{
		Root:         server.workspace.Root,
		ConfigFile:   config.FileName,
		ConfigExists: exists,
		Drivers:      database.Drivers(),
		Connections:  append([]config.Connection{}, loaded.Connections...),
		Files:        []workspaceFile{},
	}
	if loadErr != nil {
		result.ConfigError = loadErr.Error()
	}
	arranged, revision, arrangementErr := arrangement.Load(server.workspace.Root)
	result.Arrangement = arranged
	result.ArrangementFile = arrangement.FileName
	result.ArrangementRevision = revision
	if arrangementErr != nil {
		result.ArrangementError = arrangementErr.Error()
	}
	// Connections that are arranged but not configured here still own
	// their directory, so a workspace unpacked elsewhere shows its files.
	drivers := map[string]config.Driver{}
	if arranged != nil {
		for name, connection := range arranged.Connections {
			drivers[name] = connection.Driver
		}
	}
	for _, connection := range loaded.Connections {
		drivers[connection.Name] = connection.Driver
	}
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	files, err := server.workspace.Scan(names)
	if err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	for _, file := range files {
		entry := workspaceFile{File: file}
		if file.Size <= maxCountedFileBytes {
			if content, err := server.workspace.Read(file.Path); err == nil {
				count := len(sqlscript.Split(string(content), database.Dialect(drivers[file.Connection])))
				entry.Statements = &count
			}
		}
		result.Files = append(result.Files, entry)
	}
	writeJSON(response, http.StatusOK, result)
}

type saveArrangementRequest struct {
	// BaseRevision is the revision the client edited; a different one on
	// disk means someone else changed the file in the meantime.
	BaseRevision string                  `json:"baseRevision"`
	Arrangement  arrangement.Arrangement `json:"arrangement"`
}

func (server *Server) saveArrangement(response http.ResponseWriter, request *http.Request) {
	var body saveArrangementRequest
	if err := readJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	server.arrangementMu.Lock()
	defer server.arrangementMu.Unlock()
	_, current, err := arrangement.Load(server.workspace.Root)
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	if current != body.BaseRevision {
		writeError(response, http.StatusConflict, arrangement.FileName+" 已在别处修改")
		return
	}
	revision, err := arrangement.Save(server.workspace.Root, body.Arrangement)
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"revision": revision})
}

type saveConnectionRequest struct {
	PreviousName string            `json:"previousName"`
	Connection   config.Connection `json:"connection"`
}

func (server *Server) saveConnection(response http.ResponseWriter, request *http.Request) {
	var body saveConnectionRequest
	if err := readJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	server.configMu.Lock()
	defer server.configMu.Unlock()
	loaded, _, err := config.Load(server.workspace.Root)
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	next, err := loaded.Upsert(body.PreviousName, body.Connection)
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.Save(server.workspace.Root, next); err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, body.Connection)
}

func (server *Server) deleteConnection(response http.ResponseWriter, request *http.Request) {
	name := request.PathValue("name")
	server.configMu.Lock()
	defer server.configMu.Unlock()
	loaded, _, err := config.Load(server.workspace.Root)
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	if _, ok := loaded.Find(name); !ok {
		writeError(response, http.StatusNotFound, "连接不存在")
		return
	}
	if err := config.Save(server.workspace.Root, loaded.Remove(name)); err != nil {
		writeError(response, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) testConnection(response http.ResponseWriter, request *http.Request) {
	var connection config.Connection
	if err := readJSON(response, request, &connection); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	result, err := database.Test(ctx, connection)
	if err != nil {
		writeError(response, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(response, http.StatusOK, result)
}

type fileResponse struct {
	Path       string                `json:"path"`
	Content    string                `json:"content"`
	Truncated  bool                  `json:"truncated"`
	Statements []sqlscript.Statement `json:"statements"`
}

func (server *Server) getFile(response http.ResponseWriter, request *http.Request) {
	relative := request.URL.Query().Get("path")
	content, err := server.workspace.Read(relative)
	if errors.Is(err, os.ErrNotExist) {
		writeError(response, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	dialect := database.Dialect(config.Driver(request.URL.Query().Get("driver")))
	result := fileResponse{
		Path:       relative,
		Content:    string(content),
		Statements: sqlscript.Split(string(content), dialect),
	}
	if len(content) > maxViewedFileBytes {
		cut := bytes.LastIndexByte(content[:maxViewedFileBytes], '\n')
		if cut < 0 {
			cut = maxViewedFileBytes
		}
		result.Content = string(content[:cut])
		result.Truncated = true
	}
	writeJSON(response, http.StatusOK, result)
}

func (server *Server) importFiles(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, maxImportBytes)
	reader, err := request.MultipartReader()
	if err != nil {
		writeError(response, http.StatusBadRequest, "需要 multipart 表单")
		return
	}
	imported := []string{}
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if part.FileName() == "" {
			continue
		}
		name, err := server.workspace.Import(part.FileName(), part)
		part.Close()
		if err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		imported = append(imported, name)
	}
	writeJSON(response, http.StatusOK, map[string][]string{"paths": imported})
}

func (server *Server) startRun(response http.ResponseWriter, request *http.Request) {
	var plan runner.Plan
	if err := readJSON(response, request, &plan); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	loaded, _, err := config.Load(server.workspace.Root)
	if err != nil {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	connection, ok := loaded.Find(plan.Connection)
	if !ok {
		writeError(response, http.StatusNotFound, "连接不存在")
		return
	}
	run, err := server.runs.Start(plan, connection)
	if errors.Is(err, runner.ErrBusy) {
		writeError(response, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(response, http.StatusCreated, run)
}

func (server *Server) getRun(response http.ResponseWriter, request *http.Request) {
	run, ok := server.runs.Get(request.PathValue("connection"))
	if !ok {
		writeError(response, http.StatusNotFound, "没有运行记录")
		return
	}
	writeJSON(response, http.StatusOK, run)
}

func (server *Server) stopRun(response http.ResponseWriter, request *http.Request) {
	if err := server.runs.Stop(request.PathValue("connection")); err != nil {
		writeError(response, http.StatusNotFound, err.Error())
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
