package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/migration"
)

const (
	defaultParallelism        = 4
	defaultMaxOpenConnections = 4
	defaultConnectionTimeout  = 5 * time.Second
	currentDatabaseVersion    = 1
)

var environmentVariablePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func Load(ctx context.Context, graphPath, databasesPath string) (migration.Project, error) {
	ctx = nonNilContext(ctx)
	if err := contextError(ctx); err != nil {
		return migration.Project{}, err
	}
	absGraphPath, graphData, err := readConfigFile(ctx, graphPath, "graph path")
	if err != nil {
		return migration.Project{}, err
	}
	absDatabasesPath, databasesData, err := readConfigFile(ctx, databasesPath, "databases path")
	if err != nil {
		return migration.Project{}, err
	}
	graphRoot := filepath.Dir(absGraphPath)
	graphRootReal, err := filepath.EvalSymlinks(graphRoot)
	if err != nil {
		return migration.Project{}, wrapField(absGraphPath, "root directory", err)
	}
	graph, err := loadGraph(ctx, absGraphPath, graphRoot, graphRootReal, graphData)
	if err != nil {
		return migration.Project{}, err
	}
	databases, err := loadDatabases(absDatabasesPath, databasesData)
	if err != nil {
		return migration.Project{}, err
	}
	project := migration.Project{
		Graph:         graph,
		Databases:     databases,
		GraphPath:     absGraphPath,
		DatabasesPath: absDatabasesPath,
		RootDirectory: graphRoot,
	}
	if err := migration.ValidateProject(project); err != nil {
		return migration.Project{}, wrapField(absGraphPath, "project", err)
	}
	if err := contextError(ctx); err != nil {
		return migration.Project{}, err
	}
	project.Fingerprint, err = fingerprint(graph)
	if err != nil {
		return migration.Project{}, wrapField(absGraphPath, "fingerprint", err)
	}
	project.StructureFingerprint, err = structureFingerprint(graph)
	if err != nil {
		return migration.Project{}, wrapField(absGraphPath, "structure fingerprint", err)
	}
	project.EnvironmentFingerprint, err = environmentFingerprint(databases)
	if err != nil {
		return migration.Project{}, wrapField(absDatabasesPath, "environment fingerprint", err)
	}
	return project, nil
}

func loadGraph(ctx context.Context, graphPath, graphRoot, graphRootReal string, data []byte) (migration.Graph, error) {
	graph, err := loadGraphDefinition(ctx, graphPath, graphRoot, data)
	if err != nil {
		return migration.Graph{}, err
	}
	for nodeIndex := range graph.Nodes {
		for scriptIndex, script := range graph.Nodes[nodeIndex].Scripts {
			if err := contextError(ctx); err != nil {
				return migration.Graph{}, err
			}
			_, content, loadErr := loadScript(graphRoot, graphRootReal, script.Path)
			if loadErr != nil {
				fieldPath := fmt.Sprintf("nodes.%s.scripts[%d]", graph.Nodes[nodeIndex].Name, scriptIndex)
				return migration.Graph{}, wrapField(graphPath, fieldPath, loadErr)
			}
			graph.Nodes[nodeIndex].Scripts[scriptIndex] = loadedScript(script.Path, content)
		}
	}
	return graph, nil
}

func loadGraphDefinition(ctx context.Context, graphPath, graphRoot string, data []byte) (migration.Graph, error) {
	config, err := decodeGraph(data)
	if err != nil {
		return migration.Graph{}, wrapField(graphPath, "yaml", err)
	}
	if config.Version == nil {
		return migration.Graph{}, wrapField(graphPath, "version", errors.New("field is required"))
	}
	version := *config.Version
	if version != migration.CurrentGraphVersion {
		return migration.Graph{}, wrapField(graphPath, "version", fmt.Errorf("must be %d", migration.CurrentGraphVersion))
	}
	if config.Name == nil {
		return migration.Graph{}, wrapField(graphPath, "name", errors.New("field is required"))
	}
	name := *config.Name
	parallelism := defaultParallelism
	if config.Parallelism != nil {
		parallelism = *config.Parallelism
	}
	if parallelism < 1 {
		return migration.Graph{}, wrapField(graphPath, "parallelism", errors.New("must be at least 1"))
	}
	onError := migration.ErrorPolicyHalt
	if config.OnError != nil {
		onError = migration.ErrorPolicy(*config.OnError)
	}
	if !onError.IsValid() {
		return migration.Graph{}, wrapField(graphPath, "on_error", errors.New("must be halt or continue"))
	}
	if config.Nodes == nil {
		return migration.Graph{}, wrapField(graphPath, "nodes", errors.New("field is required"))
	}
	nodeNames := make([]string, 0, len(config.Nodes))
	for nodeName := range config.Nodes {
		nodeNames = append(nodeNames, nodeName)
	}
	sort.Strings(nodeNames)
	nodes := make([]migration.Node, 0, len(nodeNames))
	for _, nodeName := range nodeNames {
		if err := contextError(ctx); err != nil {
			return migration.Graph{}, err
		}
		node, nodeErr := loadNodeDefinition(config.Nodes[nodeName], nodeName, graphPath, graphRoot)
		if nodeErr != nil {
			return migration.Graph{}, nodeErr
		}
		nodes = append(nodes, node)
	}
	return migration.Graph{Version: version, Name: name, Parallelism: parallelism, OnError: onError, Nodes: nodes}, nil
}

func loadNodeDefinition(config nodeConfig, nodeName, graphPath, graphRoot string) (migration.Node, error) {
	fieldPath := fmt.Sprintf("nodes.%s", nodeName)
	if config.Database == nil {
		return migration.Node{}, wrapField(graphPath, fieldPath+".database", errors.New("field is required"))
	}
	onError := migration.ErrorPolicy("")
	if config.OnError != nil {
		onError = migration.ErrorPolicy(*config.OnError)
	}
	if onError != "" && !onError.IsValid() {
		return migration.Node{}, wrapField(graphPath, fieldPath+".on_error", errors.New("must be halt or continue"))
	}
	if config.Scripts == nil {
		return migration.Node{}, wrapField(graphPath, fieldPath+".scripts", errors.New("field is required"))
	}
	scripts := make([]migration.Script, 0, len(config.Scripts))
	for index, rawPath := range config.Scripts {
		scriptPath := fmt.Sprintf("%s.scripts[%d]", fieldPath, index)
		relativePath, resolveErr := normalizeScriptReference(graphRoot, rawPath)
		if resolveErr != nil {
			return migration.Node{}, wrapField(graphPath, scriptPath, resolveErr)
		}
		scripts = append(scripts, migration.Script{Path: relativePath})
	}
	return migration.Node{Name: nodeName, Database: *config.Database, DependsOn: config.DependsOn, Scripts: scripts, ErrorPolicy: onError}, nil
}

func loadScript(root, rootReal, rawPath string) (string, []byte, error) {
	relativePath, err := normalizeScriptReference(root, rawPath)
	if err != nil {
		return "", nil, err
	}
	cleanPath := filepath.Join(root, filepath.FromSlash(relativePath))
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", nil, fmt.Errorf("read SQL file: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve SQL file: %w", err)
	}
	if !withinDirectory(rootReal, realPath) {
		return "", nil, errors.New("path escapes the graph directory through a symbolic link")
	}
	return relativePath, content, nil
}

func normalizeScriptReference(root, rawPath string) (string, error) {
	if rawPath == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(rawPath) || path.IsAbs(filepath.ToSlash(rawPath)) || windowsAbsolutePath(rawPath) {
		return "", errors.New("path must be relative to the graph directory")
	}
	cleanPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(rawPath)))
	relativePath, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the graph directory")
	}
	if filepath.Ext(relativePath) != ".sql" {
		return "", errors.New("path must use the .sql extension")
	}
	return filepath.ToSlash(relativePath), nil
}

func loadedScript(path string, content []byte) migration.Script {
	sum := sha256.Sum256(content)
	return migration.Script{Path: path, Checksum: hex.EncodeToString(sum[:]), SQL: string(content)}
}

func loadDatabases(databasesPath string, data []byte) (map[string]migration.DatabaseProfile, error) {
	document, err := decodeDatabases(data)
	if err != nil {
		return nil, wrapField(databasesPath, "toml", err)
	}
	if document.Version == nil {
		return nil, wrapField(databasesPath, "version", errors.New("field is required"))
	}
	if *document.Version != currentDatabaseVersion {
		return nil, wrapField(databasesPath, "version", fmt.Errorf("must be %d", currentDatabaseVersion))
	}
	profiles := make(map[string]migration.DatabaseProfile, len(document.Databases))
	names := make([]string, 0, len(document.Databases))
	for name := range document.Databases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		profileValues := document.Databases[name]
		fieldPath := fmt.Sprintf("databases.%s", name)
		if !migration.ValidName(name) {
			return nil, wrapField(databasesPath, fieldPath, errors.New("name must start with a letter and contain only letters, digits, underscores, or hyphens"))
		}
		if profileValues.Driver == nil {
			return nil, wrapField(databasesPath, fieldPath+".driver", errors.New("field is required"))
		}
		if profileValues.DSN == nil {
			return nil, wrapField(databasesPath, fieldPath+".dsn", errors.New("field is required"))
		}
		driver := *profileValues.Driver
		dsn, err := expandEnvironment(*profileValues.DSN)
		if err != nil {
			return nil, wrapField(databasesPath, fieldPath+".dsn", err)
		}
		if strings.TrimSpace(dsn) == "" {
			return nil, wrapField(databasesPath, fieldPath+".dsn", errors.New("must not be empty"))
		}
		if !migration.Driver(driver).IsValid() {
			return nil, wrapField(databasesPath, fieldPath+".driver", fmt.Errorf("unsupported driver %q", driver))
		}
		maxConnections := defaultMaxOpenConnections
		if profileValues.MaxOpenConnections != nil {
			maxConnections = *profileValues.MaxOpenConnections
		}
		if maxConnections < 1 {
			return nil, wrapField(databasesPath, fieldPath+".max_open_connections", errors.New("must be at least 1"))
		}
		timeout := defaultConnectionTimeout
		if profileValues.ConnectionTimeout != nil {
			timeout, err = time.ParseDuration(*profileValues.ConnectionTimeout)
			if err != nil {
				return nil, wrapField(databasesPath, fieldPath+".connection_timeout", fmt.Errorf("must be a duration: %w", err))
			}
		}
		if timeout <= 0 {
			return nil, wrapField(databasesPath, fieldPath+".connection_timeout", errors.New("must be greater than zero"))
		}
		profiles[name] = migration.DatabaseProfile{Name: name, Driver: migration.Driver(driver), DSN: dsn, MaxOpenConnections: maxConnections, ConnectionTimeout: timeout}
	}
	return profiles, nil
}

func expandEnvironment(value string) (string, error) {
	var missing string
	result := environmentVariablePattern.ReplaceAllStringFunc(value, func(match string) string {
		name := environmentVariablePattern.FindStringSubmatch(match)[1]
		value, exists := os.LookupEnv(name)
		if !exists {
			missing = name
			return match
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %q is not set", missing)
	}
	return result, nil
}

type fingerprintScript struct {
	Path     string `json:"path"`
	Checksum string `json:"checksum,omitempty"`
}

type fingerprintNode struct {
	Name      string                `json:"name"`
	Database  string                `json:"database"`
	DependsOn []string              `json:"depends_on"`
	Scripts   []fingerprintScript   `json:"scripts"`
	OnError   migration.ErrorPolicy `json:"on_error,omitempty"`
}

type fingerprintGraph struct {
	Version     int                   `json:"version"`
	Name        string                `json:"name"`
	Parallelism int                   `json:"parallelism"`
	OnError     migration.ErrorPolicy `json:"on_error"`
	Nodes       []fingerprintNode     `json:"nodes"`
}

type structureFingerprintNode struct {
	Name      string              `json:"name"`
	Database  string              `json:"database"`
	DependsOn []string            `json:"depends_on"`
	Scripts   []fingerprintScript `json:"scripts"`
}

type structureFingerprintGraph struct {
	Version int                        `json:"version"`
	Name    string                     `json:"name"`
	Nodes   []structureFingerprintNode `json:"nodes"`
}

func fingerprint(graph migration.Graph) (string, error) {
	nodes := make([]fingerprintNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		dependencies := append([]string(nil), node.DependsOn...)
		sort.Strings(dependencies)
		scripts := make([]fingerprintScript, 0, len(node.Scripts))
		for _, script := range node.Scripts {
			scripts = append(scripts, fingerprintScript{Path: normalizeScriptPath(script.Path), Checksum: script.Checksum})
		}
		nodes = append(nodes, fingerprintNode{Name: node.Name, Database: node.Database, DependsOn: dependencies, Scripts: scripts, OnError: node.EffectiveErrorPolicy(graph.OnError)})
	}
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].Name < nodes[right].Name })
	canonical := fingerprintGraph{Version: graph.Version, Name: graph.Name, Parallelism: graph.Parallelism, OnError: graph.OnError, Nodes: nodes}
	return digest(canonical)
}

func structureFingerprint(graph migration.Graph) (string, error) {
	nodes := make([]structureFingerprintNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		dependencies := append([]string(nil), node.DependsOn...)
		sort.Strings(dependencies)
		scripts := make([]fingerprintScript, 0, len(node.Scripts))
		for _, script := range node.Scripts {
			scripts = append(scripts, fingerprintScript{Path: normalizeScriptPath(script.Path)})
		}
		nodes = append(nodes, structureFingerprintNode{Name: node.Name, Database: node.Database, DependsOn: dependencies, Scripts: scripts})
	}
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].Name < nodes[right].Name })
	canonical := structureFingerprintGraph{Version: graph.Version, Name: graph.Name, Nodes: nodes}
	return digest(canonical)
}

func digest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeScriptPath(value string) string {
	return filepath.ToSlash(filepath.Clean(value))
}

type environmentProfileFingerprint struct {
	Name   string           `json:"name"`
	Driver migration.Driver `json:"driver"`
	DSN    string           `json:"dsn"`
}

func environmentFingerprint(databases map[string]migration.DatabaseProfile) (string, error) {
	names := make([]string, 0, len(databases))
	for name := range databases {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]environmentProfileFingerprint, 0, len(names))
	for _, name := range names {
		profile := databases[name]
		profiles = append(profiles, environmentProfileFingerprint{Name: name, Driver: profile.Driver, DSN: profile.DSN})
	}
	data, err := json.Marshal(profiles)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validateGraphForSave(graphPath string, graph migration.Graph) error {
	root := filepath.Dir(graphPath)
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return wrapField(graphPath, "root directory", err)
	}
	validated := graph
	validated.Nodes = make([]migration.Node, len(graph.Nodes))
	databases := make(map[string]migration.DatabaseProfile)
	for index, node := range graph.Nodes {
		if strings.TrimSpace(node.Database) == "" {
			return wrapField(graphPath, fmt.Sprintf("nodes.%s.database", node.Name), errors.New("must not be empty"))
		}
		validatedNode := node
		validatedNode.Scripts = make([]migration.Script, len(node.Scripts))
		for scriptIndex, script := range node.Scripts {
			relativePath, content, loadErr := loadScript(root, rootReal, script.Path)
			if loadErr != nil {
				return wrapField(graphPath, fmt.Sprintf("nodes.%s.scripts[%d]", node.Name, scriptIndex), loadErr)
			}
			sum := sha256.Sum256(content)
			validatedNode.Scripts[scriptIndex] = migration.Script{Path: relativePath, Checksum: hex.EncodeToString(sum[:]), SQL: string(content)}
		}
		validated.Nodes[index] = validatedNode
		if _, exists := databases[node.Database]; !exists {
			databases[node.Database] = migration.DatabaseProfile{Name: node.Database, Driver: migration.DriverPostgres, DSN: "save://" + node.Database}
		}
	}
	if err := migration.ValidateProject(migration.Project{Graph: validated, Databases: databases}); err != nil {
		return wrapField(graphPath, "project", err)
	}
	return nil
}

func SaveGraph(ctx context.Context, graphPath string, graph migration.Graph) error {
	ctx = nonNilContext(ctx)
	if err := contextError(ctx); err != nil {
		return err
	}
	absPath, err := absolutePath(graphPath, "graph path")
	if err != nil {
		return err
	}
	if err := validateGraphForSave(absPath, graph); err != nil {
		return err
	}
	targetMode := os.FileMode(0o600)
	if info, statErr := os.Stat(absPath); statErr == nil {
		if info.IsDir() {
			return wrapField(absPath, "graph path", errors.New("must be a file"))
		}
		targetMode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return wrapField(absPath, "graph path", statErr)
	}
	data, err := marshalGraph(graph)
	if err != nil {
		return wrapField(absPath, "graph", err)
	}
	return writeConfigFile(ctx, absPath, data, targetMode)
}

func writeConfigFile(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return wrapField(path, "path", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return wrapField(path, "path", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		return wrapField(path, "path", err)
	}
	if err := temporary.Sync(); err != nil {
		return wrapField(path, "path", err)
	}
	if err := temporary.Close(); err != nil {
		return wrapField(path, "path", err)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return wrapField(path, "path", err)
	}
	removeTemporary = false
	return nil
}

func FileTree(ctx context.Context, graphPath string) ([]string, error) {
	ctx = nonNilContext(ctx)
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	absPath, err := absolutePath(graphPath, "graph path")
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(absPath)
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		root = absPath
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, wrapField(absPath, "graph path", statErr)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, wrapField(absPath, "root directory", err)
	}
	paths := make([]string, 0)
	err = filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if contextErr := contextError(ctx); contextErr != nil {
			return contextErr
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(currentPath) != ".sql" {
			return nil
		}
		realPath, resolveErr := filepath.EvalSymlinks(currentPath)
		if resolveErr != nil || !withinDirectory(rootReal, realPath) {
			return nil
		}
		relativePath, relativeErr := filepath.Rel(root, currentPath)
		if relativeErr != nil {
			return relativeErr
		}
		paths = append(paths, filepath.ToSlash(relativePath))
		return nil
	})
	if err != nil {
		return nil, wrapField(absPath, "file tree", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func absolutePath(value, label string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", wrapField(label, "", errors.New("path is required"))
	}
	absPath, err := filepath.Abs(value)
	if err != nil {
		return "", wrapField(value, label, err)
	}
	return filepath.Clean(absPath), nil
}

func readConfigFile(ctx context.Context, value, label string) (string, []byte, error) {
	absPath, err := absolutePath(value, label)
	if err != nil {
		return "", nil, err
	}
	if err := contextError(ctx); err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", nil, wrapField(absPath, label, err)
	}
	return absPath, data, nil
}

func withinDirectory(root, value string) bool {
	relative, err := filepath.Rel(root, value)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func windowsAbsolutePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func contextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
