// Package bundle moves arranged connections between workspaces: a .zip
// holding the SQL files, their arrangement and a manifest, but no
// connection details, so the receiving workspace supplies its own.
package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/schemapilot/schemapilot/internal/arrangement"
	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/workspace"
)

const (
	ManifestName = "schemapilot.package.json"
	format       = 1
	// maxEntryBytes bounds what one archive entry may expand to.
	maxEntryBytes = workspace.MaxFileBytes
)

type Manifest struct {
	Format      int          `json:"format"`
	CreatedAt   time.Time    `json:"createdAt"`
	Source      string       `json:"source"`
	Version     string       `json:"schemapilotVersion,omitempty"`
	Connections []Connection `json:"connections"`
	Files       []File       `json:"files"`
}

type Connection struct {
	Name   string        `json:"name"`
	Driver config.Driver `json:"driver,omitempty"`
}

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Export writes the named connections' arrangement and every file it
// arranges, disabled ones included.
func Export(output io.Writer, files workspace.Workspace, arranged arrangement.Arrangement, names []string, version string) error {
	subset := arrangement.Arrangement{Version: 1, Connections: map[string]arrangement.Connection{}}
	manifest := Manifest{
		Format:    format,
		CreatedAt: time.Now().UTC(),
		Source:    filepath.Base(files.Root),
		Version:   version,
	}
	if len(names) == 0 {
		return errors.New("没有选择连接")
	}
	for _, name := range names {
		connection, ok := arranged.Connections[name]
		if !ok || len(connection.Steps) == 0 {
			return fmt.Errorf("连接 %s 没有编排", name)
		}
		subset.Connections[name] = connection
		manifest.Connections = append(manifest.Connections, Connection{Name: name, Driver: connection.Driver})
	}
	paths := subset.Files(names)
	for _, path := range arranged.Detached {
		if slices.Contains(paths, path) {
			subset.Detached = append(subset.Detached, path)
		}
	}

	contents := map[string][]byte{}
	var missing []string
	for _, path := range paths {
		content, err := files.Read(path)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, path)
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		contents[path] = content
		manifest.Files = append(manifest.Files, File{Path: path, SHA256: digest(content), Size: int64(len(content))})
	}
	if len(missing) > 0 {
		return fmt.Errorf("以下文件不存在，无法打包：%s", strings.Join(missing, "、"))
	}

	archive := zip.NewWriter(output)
	manifestContent, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	arrangementContent, err := arrangement.Encode(subset)
	if err != nil {
		return err
	}
	if err := add(archive, ManifestName, append(manifestContent, '\n'), manifest.CreatedAt); err != nil {
		return err
	}
	if err := add(archive, arrangement.FileName, arrangementContent, manifest.CreatedAt); err != nil {
		return err
	}
	for _, file := range manifest.Files {
		if err := add(archive, file.Path, contents[file.Path], manifest.CreatedAt); err != nil {
			return err
		}
	}
	return archive.Close()
}

func add(archive *zip.Writer, name string, content []byte, modified time.Time) error {
	writer, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified})
	if err != nil {
		return err
	}
	_, err = writer.Write(content)
	return err
}

type Summary struct {
	Manifest Manifest
	Written  int
	Kept     int
}

// ConflictError lists everything that already exists in the workspace with
// different content; nothing is written when it is returned.
type ConflictError struct {
	Files        []string
	Arrangements []string
}

func (conflict *ConflictError) Error() string {
	var parts []string
	if len(conflict.Files) > 0 {
		parts = append(parts, "以下文件已存在且内容不同：\n  "+strings.Join(conflict.Files, "\n  "))
	}
	if len(conflict.Arrangements) > 0 {
		parts = append(parts, fmt.Sprintf("%s 中以下连接的编排与包不同：\n  %s", arrangement.FileName, strings.Join(conflict.Arrangements, "\n  ")))
	}
	return strings.Join(parts, "\n")
}

// Import unpacks a package into the workspace. Files and arrangements that
// already match are kept; any that differ abort the import before anything
// is written.
func Import(archivePath string, files workspace.Workspace) (Summary, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return Summary{}, fmt.Errorf("打开 %s: %w", archivePath, err)
	}
	defer archive.Close()

	entries := map[string]*zip.File{}
	for _, entry := range archive.File {
		entries[entry.Name] = entry
	}
	manifestContent, err := readEntry(entries, ManifestName)
	if err != nil {
		return Summary{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		return Summary{}, fmt.Errorf("解析 %s: %w", ManifestName, err)
	}
	if manifest.Format != format {
		return Summary{}, fmt.Errorf("不支持的包格式版本 %d", manifest.Format)
	}
	arrangementContent, err := readEntry(entries, arrangement.FileName)
	if err != nil {
		return Summary{}, err
	}
	var packaged arrangement.Arrangement
	if err := json.Unmarshal(arrangementContent, &packaged); err != nil {
		return Summary{}, fmt.Errorf("解析包内的 %s: %w", arrangement.FileName, err)
	}
	if err := packaged.Validate(); err != nil {
		return Summary{}, err
	}

	contents := map[string][]byte{}
	for _, file := range manifest.Files {
		if err := arrangement.ValidatePath(file.Path); err != nil || !strings.HasSuffix(strings.ToLower(file.Path), ".sql") {
			return Summary{}, fmt.Errorf("包内文件路径 %q 无效", file.Path)
		}
		content, err := readEntry(entries, file.Path)
		if err != nil {
			return Summary{}, err
		}
		if digest(content) != file.SHA256 {
			return Summary{}, fmt.Errorf("%s 的校验和与清单不符，包可能已损坏或被修改", file.Path)
		}
		contents[file.Path] = content
	}

	summary := Summary{Manifest: manifest}
	conflict := &ConflictError{}
	var pending []string
	for _, file := range manifest.Files {
		existing, err := files.Read(file.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			pending = append(pending, file.Path)
		case err != nil:
			return Summary{}, fmt.Errorf("%s: %w", file.Path, err)
		case digest(existing) == file.SHA256:
			summary.Kept++
		default:
			conflict.Files = append(conflict.Files, file.Path)
		}
	}
	current, revision, err := arrangement.Load(files.Root)
	if err != nil {
		return Summary{}, err
	}
	merged := arrangement.Arrangement{Version: 1, Connections: map[string]arrangement.Connection{}}
	if current != nil {
		merged = *current
	}
	for name, connection := range packaged.Connections {
		if existing, ok := merged.Connections[name]; ok && !sameArrangement(existing, connection) {
			conflict.Arrangements = append(conflict.Arrangements, name)
		}
		merged.Connections[name] = connection
	}
	for _, path := range packaged.Detached {
		if !slices.Contains(merged.Detached, path) {
			merged.Detached = append(merged.Detached, path)
		}
	}
	if len(conflict.Files) > 0 || len(conflict.Arrangements) > 0 {
		return Summary{}, conflict
	}

	for _, path := range pending {
		if err := writeFile(files.Root, path, contents[path]); err != nil {
			return Summary{}, err
		}
		summary.Written++
	}
	if _, latest, _ := arrangement.Load(files.Root); latest != revision {
		return Summary{}, fmt.Errorf("%s 在导入过程中被修改，请重试", arrangement.FileName)
	}
	if _, err := arrangement.Save(files.Root, merged); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func sameArrangement(a, b arrangement.Connection) bool {
	return reflect.DeepEqual(a.Steps, b.Steps) && slices.Equal(a.Disabled, b.Disabled)
}

func readEntry(entries map[string]*zip.File, name string) ([]byte, error) {
	entry, ok := entries[name]
	if !ok {
		return nil, fmt.Errorf("包内缺少 %s", name)
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("读取包内的 %s: %w", name, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, maxEntryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取包内的 %s: %w", name, err)
	}
	if len(content) > maxEntryBytes {
		return nil, fmt.Errorf("包内的 %s 超过大小上限", name)
	}
	return content, nil
}

// writeFile creates a new file below root, refusing directories that lead
// out of it through symbolic links.
func writeFile(root, path string, content []byte) error {
	target := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return err
	}
	if relative, err := filepath.Rel(root, parent); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s 所在目录超出工作目录", path)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
