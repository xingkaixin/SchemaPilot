package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/schemapilot/schemapilot/internal/runner"
	"github.com/schemapilot/schemapilot/internal/workspace"
)

const maxJSONBodyBytes = 1 << 20

type Server struct {
	workspace workspace.Workspace
	runs      *runner.Manager
	assets    fs.FS
	logger    *slog.Logger
	// loopbackOnly rejects requests whose Host is not a loopback name, which
	// stops DNS-rebinding pages from driving a locally bound server.
	loopbackOnly bool

	configMu      sync.Mutex
	arrangementMu sync.Mutex
}

func New(files workspace.Workspace, runs *runner.Manager, assets fs.FS, logger *slog.Logger, loopbackOnly bool) *Server {
	return &Server{workspace: files, runs: runs, assets: assets, logger: logger, loopbackOnly: loopbackOnly}
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/workspace", server.getWorkspace)
	mux.HandleFunc("POST /api/connections", server.saveConnection)
	mux.HandleFunc("DELETE /api/connections/{name}", server.deleteConnection)
	mux.HandleFunc("POST /api/connections/test", server.testConnection)
	mux.HandleFunc("PUT /api/arrangement", server.saveArrangement)
	mux.HandleFunc("GET /api/file", server.getFile)
	mux.HandleFunc("POST /api/files", server.importFiles)
	mux.HandleFunc("POST /api/runs", server.startRun)
	mux.HandleFunc("GET /api/runs/{connection}", server.getRun)
	mux.HandleFunc("POST /api/runs/{connection}/stop", server.stopRun)
	mux.Handle("/", server.static())
	return server.recoverPanics(server.secure(mux))
}

func (server *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; font-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		if server.loopbackOnly && !isLoopbackHost(request.Host) {
			writeError(response, http.StatusForbidden, "只接受 localhost 访问")
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/") {
			response.Header().Set("Cache-Control", "no-store")
			if request.Method != http.MethodGet && !sameOrigin(request) {
				writeError(response, http.StatusForbidden, "不接受跨站请求")
				return
			}
		}
		next.ServeHTTP(response, request)
	})
}

func (server *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				server.logger.Error("http handler panic", "path", request.URL.Path, "error", recovered)
				writeError(response, http.StatusInternalServerError, "服务器内部错误")
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func (server *Server) static() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if server.assets == nil {
			http.NotFound(response, request)
			return
		}
		requested := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
		if requested == "" {
			requested = "index.html"
		}
		content, err := fs.ReadFile(server.assets, requested)
		if err != nil {
			requested = "index.html"
			content, err = fs.ReadFile(server.assets, requested)
		}
		if err != nil {
			http.NotFound(response, request)
			return
		}
		if contentType := mime.TypeByExtension(path.Ext(requested)); contentType != "" {
			response.Header().Set("Content-Type", contentType)
		}
		if requested == "index.html" {
			response.Header().Set("Cache-Control", "no-cache")
		} else {
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeContent(response, request, requested, time.Time{}, bytes.NewReader(content))
	})
}

func sameOrigin(request *http.Request) bool {
	if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == request.Host
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if parsed, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func readJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("请求格式错误: " + err.Error())
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("请求体只能包含一个 JSON 值")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	encoder := json.NewEncoder(response)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}
