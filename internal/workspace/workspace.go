package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const MaxFileBytes = 64 << 20

type File struct {
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"modTime"`
	Connection string    `json:"connection,omitempty"`
}

type Workspace struct {
	Root string
}

func Open(dir string) (Workspace, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return Workspace{}, err
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Workspace{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Workspace{}, err
	}
	if !info.IsDir() {
		return Workspace{}, fmt.Errorf("%s 不是目录", root)
	}
	return Workspace{Root: root}, nil
}

// Scan lists .sql files directly under the root as unassigned and walks
// each directory named after a connection recursively, assigning its files
// to that connection.
func (workspace Workspace) Scan(connections []string) ([]File, error) {
	entries, err := os.ReadDir(workspace.Root)
	if err != nil {
		return nil, fmt.Errorf("读取目录: %w", err)
	}
	var files []File
	for _, entry := range entries {
		if entry.Type().IsRegular() && isSQL(entry.Name()) {
			if file, ok := workspace.stat(entry.Name(), ""); ok {
				files = append(files, file)
			}
		}
	}
	for _, connection := range connections {
		dir := filepath.Join(workspace.Root, connection)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		err := filepath.WalkDir(dir, func(current string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if current != dir && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || !isSQL(entry.Name()) {
				return nil
			}
			relative, err := filepath.Rel(workspace.Root, current)
			if err != nil {
				return nil
			}
			if file, ok := workspace.stat(filepath.ToSlash(relative), connection); ok {
				files = append(files, file)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("扫描 %s: %w", connection, err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (workspace Workspace) stat(relative, connection string) (File, bool) {
	info, err := os.Stat(filepath.Join(workspace.Root, filepath.FromSlash(relative)))
	if err != nil {
		return File{}, false
	}
	return File{Path: relative, Size: info.Size(), ModTime: info.ModTime().UTC(), Connection: connection}, true
}

// Resolve maps a workspace-relative .sql path to an absolute path, refusing
// paths that leave the root directly or through symbolic links.
func (workspace Workspace) Resolve(relative string) (string, error) {
	if relative == "" || path.IsAbs(relative) || filepath.IsAbs(relative) || !isSQL(relative) {
		return "", errors.New("路径必须是工作目录内的 .sql 文件")
	}
	absolute := filepath.Join(workspace.Root, filepath.FromSlash(path.Clean(relative)))
	if !within(workspace.Root, absolute) {
		return "", errors.New("路径超出工作目录")
	}
	real, err := filepath.EvalSymlinks(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("文件不存在: %s: %w", relative, os.ErrNotExist)
	}
	if err != nil {
		return "", err
	}
	if !within(workspace.Root, real) {
		return "", errors.New("路径通过符号链接超出工作目录")
	}
	return real, nil
}

func (workspace Workspace) Read(relative string) ([]byte, error) {
	absolute, err := workspace.Resolve(relative)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > MaxFileBytes {
		return nil, fmt.Errorf("%s 超过 %d MB", relative, MaxFileBytes>>20)
	}
	return content, nil
}

// Import copies a dropped file into the root. An existing file is never
// overwritten: the copy gets a numeric suffix instead.
func (workspace Workspace) Import(name string, content io.Reader) (string, error) {
	base := filepath.Base(filepath.Clean(name))
	if !isSQL(base) || strings.HasPrefix(base, ".") {
		return "", fmt.Errorf("只能导入 .sql 文件: %s", name)
	}
	extension := filepath.Ext(base)
	stem := strings.TrimSuffix(base, extension)
	for attempt := 0; attempt < 1000; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, attempt, extension)
		}
		target := filepath.Join(workspace.Root, candidate)
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(file, io.LimitReader(content, MaxFileBytes))
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			os.Remove(target)
			return "", err
		}
		return candidate, nil
	}
	return "", fmt.Errorf("无法为 %s 生成不重名的文件名", name)
}

func isSQL(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".sql")
}

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
