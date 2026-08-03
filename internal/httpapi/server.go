package httpapi

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/execution"
)

type Config struct {
	Context       context.Context
	Engine        *execution.Engine
	Connector     execution.DatabaseConnector
	GraphPath     string
	DatabasesPath string
	Assets        fs.FS
	Logger        *slog.Logger
}

type Server struct {
	context       context.Context
	engine        *execution.Engine
	connector     execution.DatabaseConnector
	graphPath     string
	databasesPath string
	assets        fs.FS
	logger        *slog.Logger
}

func New(config Config) (*Server, error) {
	if config.Engine == nil {
		return nil, errors.New("execution engine is required")
	}
	if config.Connector == nil {
		return nil, errors.New("database connector is required")
	}
	if strings.TrimSpace(config.GraphPath) == "" {
		return nil, errors.New("graph path is required")
	}
	if strings.TrimSpace(config.DatabasesPath) == "" {
		return nil, errors.New("databases path is required")
	}
	if config.Context == nil {
		config.Context = context.Background()
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &Server{
		context:       config.Context,
		engine:        config.Engine,
		connector:     config.Connector,
		graphPath:     config.GraphPath,
		databasesPath: config.DatabasesPath,
		assets:        config.Assets,
		logger:        config.Logger,
	}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/project", server.getProject)
	mux.HandleFunc("PUT /api/v1/graph", server.putGraph)
	mux.HandleFunc("GET /api/v1/scripts", server.getScript)
	mux.HandleFunc("PUT /api/v1/scripts", server.putScript)
	mux.HandleFunc("POST /api/v1/databases/{name}/test", server.testDatabase)
	mux.HandleFunc("GET /api/v1/runs", server.getRuns)
	mux.HandleFunc("POST /api/v1/runs", server.startRun)
	mux.HandleFunc("GET /api/v1/runs/{id}", server.getRun)
	mux.HandleFunc("POST /api/v1/runs/{id}/resume", server.resumeRun)
	mux.HandleFunc("GET /healthz", server.health)
	mux.Handle("/", server.staticHandler())

	return server.recoverPanics(server.logRequests(server.secure(mux)))
}

func (server *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; font-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; worker-src 'self' blob:")
		if strings.HasPrefix(request.URL.Path, "/api/") {
			response.Header().Set("Cache-Control", "no-store")
		}
		if isMutation(request.Method) && request.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(response, http.StatusForbidden, "cross-site mutations are not allowed")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (server *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		tracked := &statusWriter{ResponseWriter: response, status: http.StatusOK}
		next.ServeHTTP(tracked, request)
		server.logger.InfoContext(request.Context(), "http request",
			"method", request.Method,
			"path", request.URL.Path,
			"status", tracked.status,
			"duration", time.Since(startedAt),
		)
	})
}

func (server *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				server.logger.ErrorContext(request.Context(), "http handler panic", "error", recovered)
				writeError(response, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}
