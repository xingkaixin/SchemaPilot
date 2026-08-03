package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/schemapilot/schemapilot/internal/database"
	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
	"github.com/schemapilot/schemapilot/internal/runstore"
)

func TestProjectEndpointMasksDSNAndSetsETag(t *testing.T) {
	fixture := newHTTPFixture(t, "SELECT 1;", nil)
	response := fixture.request(http.MethodGet, "/api/v1/project", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET project status = %d, body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("ETag") == "" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("project headers = %#v", response.Header())
	}

	var body projectResponse
	decodeResponse(t, response, &body)
	if len(body.Databases) != 1 {
		t.Fatalf("databases = %+v", body.Databases)
	}
	if strings.Contains(body.Databases[0].DSN, "super-secret") {
		t.Fatalf("DSN leaked password: %q", body.Databases[0].DSN)
	}
	if body.Databases[0].DSN != "postgres://alice:%2A%2A%2A@db.example.com/app?sslmode=require" {
		t.Fatalf("masked DSN = %q", body.Databases[0].DSN)
	}
}

func TestScriptEndpointsUseETagAndRejectEscapes(t *testing.T) {
	fixture := newHTTPFixture(t, "SELECT 1;", nil)
	getResponse := fixture.request(http.MethodGet, "/api/v1/scripts?path=user.sql", nil, nil)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET script status = %d, body=%s", getResponse.Code, getResponse.Body.String())
	}
	var script scriptResponse
	decodeResponse(t, getResponse, &script)
	oldETag := getResponse.Header().Get("ETag")
	if script.Content != "SELECT 1;" || oldETag == "" {
		t.Fatalf("script response = %+v, ETag=%q", script, oldETag)
	}

	updated := fixture.request(http.MethodPut, "/api/v1/scripts?path=user.sql", `{"content":"SELECT 2;"}`, map[string]string{"If-Match": oldETag})
	if updated.Code != http.StatusOK {
		t.Fatalf("PUT script status = %d, body=%s", updated.Code, updated.Body.String())
	}
	if updated.Header().Get("ETag") == oldETag {
		t.Fatalf("script ETag did not change: %q", oldETag)
	}

	stale := fixture.request(http.MethodPut, "/api/v1/scripts?path=user.sql", `{"content":"SELECT 3;"}`, map[string]string{"If-Match": oldETag})
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale PUT script status = %d, body=%s", stale.Code, stale.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(fixture.root, "user.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "SELECT 2;" {
		t.Fatalf("stale PUT changed file: %q", content)
	}

	unknown := fixture.request(http.MethodPut, "/api/v1/scripts?path=user.sql", `{"content":"SELECT 4;","extra":true}`, nil)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown script DTO status = %d, body=%s", unknown.Code, unknown.Body.String())
	}

	outside := filepath.Join(t.TempDir(), "outside.sql")
	if err := os.WriteFile(outside, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture.root, "escape.sql")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	for _, requestPath := range []string{"../outside.sql", "escape.sql", filepath.Join(fixture.root, "user.sql")} {
		response := fixture.request(http.MethodGet, "/api/v1/scripts?path="+requestPath, nil, nil)
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET escaped script %q status = %d, body=%s", requestPath, response.Code, response.Body.String())
		}
		response = fixture.request(http.MethodPut, "/api/v1/scripts?path="+requestPath, `{"content":"OVERWRITE"}`, map[string]string{"If-Match": "*"})
		if response.Code != http.StatusBadRequest {
			t.Errorf("PUT escaped script %q status = %d, body=%s", requestPath, response.Code, response.Body.String())
		}
	}
}

func TestGraphPutUsesETagAndStrictDTO(t *testing.T) {
	fixture := newHTTPFixture(t, "SELECT 1;", nil)
	projectResponse := fixture.request(http.MethodGet, "/api/v1/project", nil, nil)
	currentETag := projectResponse.Header().Get("ETag")

	unknown := fixture.request(http.MethodPut, "/api/v1/graph", `{"version":1,"name":"shop","parallelism":2,"on_error":"halt","nodes":[],"extra":true}`, map[string]string{"If-Match": currentETag})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown graph DTO status = %d, body=%s", unknown.Code, unknown.Body.String())
	}

	valid := `{"version":1,"name":"shop","parallelism":3,"on_error":"halt","nodes":[{"name":"user","database":"primary","depends_on":[],"scripts":[{"path":"user.sql"}]}]}`
	updated := fixture.request(http.MethodPut, "/api/v1/graph", valid, map[string]string{"If-Match": currentETag})
	if updated.Code != http.StatusOK {
		t.Fatalf("valid graph PUT status = %d, body=%s", updated.Code, updated.Body.String())
	}
	updatedETag := updated.Header().Get("ETag")
	if updatedETag == "" || updatedETag == currentETag {
		t.Fatalf("graph ETag = %q, previous=%q", updatedETag, currentETag)
	}

	stale := fixture.request(http.MethodPut, "/api/v1/graph", valid, map[string]string{"If-Match": currentETag})
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale graph PUT status = %d, body=%s", stale.Code, stale.Body.String())
	}
}

func TestRunStartListDetailAndResume(t *testing.T) {
	fixture := newHTTPFixture(t, "SELECT 1;", nil)
	fixture.target.SetScriptError("user.sql", errors.New("script failed"))

	start := fixture.request(http.MethodPost, "/api/v1/runs", `{}`, nil)
	if start.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, body=%s", start.Code, start.Body.String())
	}
	var accepted runAcceptedResponse
	decodeResponse(t, start, &accepted)
	failed := fixture.waitForRun(accepted.ID, migration.RunStatusFailed)
	if failed.Run.Attempt != 1 || len(failed.Attempts) != 1 {
		t.Fatalf("failed run snapshot = %+v", failed)
	}

	list := fixture.request(http.MethodGet, "/api/v1/runs", nil, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", list.Code, list.Body.String())
	}
	var listed runsResponse
	decodeResponse(t, list, &listed)
	if len(listed.Runs) != 1 || listed.Runs[0].ID != accepted.ID || listed.Runs[0].Status != migration.RunStatusFailed {
		t.Fatalf("run list = %+v", listed)
	}

	fixture.target.SetScriptError("user.sql", nil)
	resume := fixture.request(http.MethodPost, "/api/v1/runs/"+string(accepted.ID)+"/resume", `{}`, nil)
	if resume.Code != http.StatusAccepted {
		t.Fatalf("resume status = %d, body=%s", resume.Code, resume.Body.String())
	}
	resumed := fixture.waitForRun(accepted.ID, migration.RunStatusSucceeded)
	if resumed.Run.Attempt != 2 || len(resumed.Attempts) != 2 {
		t.Fatalf("resumed snapshot = %+v", resumed)
	}

	missing := fixture.request(http.MethodGet, "/api/v1/runs/missing", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing run status = %d, body=%s", missing.Code, missing.Body.String())
	}
}

func TestDatabaseTestEndpointUsesFakeConnector(t *testing.T) {
	fixture := newHTTPFixture(t, "SELECT 1;", nil)
	response := fixture.request(http.MethodPost, "/api/v1/databases/primary/test", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("database test status = %d, body=%s", response.Code, response.Body.String())
	}
	var body databaseTestResponse
	decodeResponse(t, response, &body)
	if body.Name != "primary" || body.LatencyMS < 0 {
		t.Fatalf("database test response = %+v", body)
	}

	missing := fixture.request(http.MethodPost, "/api/v1/databases/unknown/test", nil, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown database test status = %d, body=%s", missing.Code, missing.Body.String())
	}
}

func TestDatabaseTestEndpointReportsPingFailure(t *testing.T) {
	fixture := newHTTPFixture(t, "SELECT 1;", nil)
	fixture.target.SetPingError(errors.New("ping failed"))

	response := fixture.request(http.MethodPost, "/api/v1/databases/primary/test", nil, nil)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("database test status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestStaticSPAFallbackAndSecurityHeaders(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    {Mode: fs.ModePerm, Data: []byte("<html>app</html>")},
		"assets/app.js": {Mode: fs.ModePerm, Data: []byte("console.log('app')")},
	}
	fixture := newHTTPFixture(t, "SELECT 1;", assets)

	root := fixture.request(http.MethodGet, "/", nil, nil)
	if root.Code != http.StatusOK || root.Body.String() != "<html>app</html>" || root.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("root response: status=%d body=%q headers=%v", root.Code, root.Body.String(), root.Header())
	}
	if root.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("root content type = %q", root.Header().Get("Content-Type"))
	}

	asset := fixture.request(http.MethodGet, "/assets/app.js", nil, nil)
	if asset.Code != http.StatusOK || asset.Body.String() != "console.log('app')" || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset response: status=%d body=%q headers=%v", asset.Code, asset.Body.String(), asset.Header())
	}

	fallback := fixture.request(http.MethodGet, "/dashboard/runs/1", nil, nil)
	if fallback.Code != http.StatusOK || fallback.Body.String() != "<html>app</html>" {
		t.Fatalf("SPA fallback: status=%d body=%q", fallback.Code, fallback.Body.String())
	}

	api := fixture.request(http.MethodGet, "/api/v1/project", nil, nil)
	if api.Header().Get("X-Content-Type-Options") != "nosniff" || api.Header().Get("Referrer-Policy") != "no-referrer" || api.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("security headers = %v", api.Header())
	}

	crossSite := fixture.request(http.MethodPost, "/api/v1/runs", `{}`, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site mutation status = %d, body=%s", crossSite.Code, crossSite.Body.String())
	}
	list := fixture.request(http.MethodGet, "/api/v1/runs", nil, nil)
	var listed runsResponse
	decodeResponse(t, list, &listed)
	if len(listed.Runs) != 0 {
		t.Fatalf("cross-site mutation created runs: %+v", listed.Runs)
	}
}

type httpFixture struct {
	root      string
	store     *runstore.Store
	connector *database.FakeConnector
	target    *database.FakeTargetDatabase
	handler   http.Handler
}

func newHTTPFixture(t *testing.T, script string, assets fs.FS) *httpFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "user.sql"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	graphPath := filepath.Join(root, "migration.yaml")
	graph := `version: 1
name: shop
parallelism: 2
on_error: halt
nodes:
  user:
    database: primary
    scripts: [user.sql]
`
	if err := os.WriteFile(graphPath, []byte(graph), 0o600); err != nil {
		t.Fatal(err)
	}
	databasesPath := filepath.Join(root, "databases.toml")
	databases := `version = 1
[databases.primary]
driver = "postgres"
dsn = "postgres://alice:super-secret@db.example.com/app?sslmode=require"
`
	if err := os.WriteFile(databasesPath, []byte(databases), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := runstore.Open(filepath.Join(root, "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	target := database.NewFakeTargetDatabase()
	connector := database.NewFakeConnector()
	if err := connector.SetDatabase("primary", target); err != nil {
		store.Close()
		t.Fatal(err)
	}
	engine := execution.NewEngine(store, connector)
	server, err := New(Config{
		Context:       context.Background(),
		Engine:        engine,
		Connector:     connector,
		GraphPath:     graphPath,
		DatabasesPath: databasesPath,
		Assets:        assets,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	fixture := &httpFixture{root: root, store: store, connector: connector, target: target, handler: server.Handler()}
	t.Cleanup(func() { _ = store.Close() })
	return fixture
}

func (fixture *httpFixture) request(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var reader io.Reader
	switch value := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(value)
	default:
		data, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		reader = strings.NewReader(string(data))
	}
	request := httptest.NewRequest(method, path, reader)
	if reader != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	return response
}

func (fixture *httpFixture) waitForRun(id execution.RunID, status migration.RunStatus) runSnapshotDTO {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		response := fixture.request(http.MethodGet, "/api/v1/runs/"+string(id), nil, nil)
		if response.Code != http.StatusOK {
			panic(fmt.Sprintf("run detail status = %d, body=%s", response.Code, response.Body.String()))
		}
		var snapshot runSnapshotDTO
		if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
			panic(err)
		}
		if snapshot.Run.Status == status {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	panic(fmt.Sprintf("run %s did not reach %s", id, status))
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}
