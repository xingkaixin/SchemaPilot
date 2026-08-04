package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxSQLFileBytes = 8 << 20

type sqlPath struct {
	absolute string
	relative string
}

func resolveSQLPath(root string, requested string) (sqlPath, error) {
	absoluteRoot, absolute, relative, err := cleanSQLPath(root, requested)
	if err != nil {
		return sqlPath{}, err
	}
	realRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve project root: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve SQL path: %w", err)
	}
	realRelative, err := filepath.Rel(realRoot, realPath)
	if err != nil || realRelative == ".." || strings.HasPrefix(realRelative, ".."+string(filepath.Separator)) {
		return sqlPath{}, errors.New("SQL path escapes the project root through a symbolic link")
	}

	return sqlPath{absolute: absolute, relative: filepath.ToSlash(relative)}, nil
}

func createSQLFile(ctx context.Context, root, requested string, content []byte) (sqlPath, error) {
	if len(content) > maxSQLFileBytes {
		return sqlPath{}, fmt.Errorf("SQL file must be at most %d bytes", maxSQLFileBytes)
	}
	if err := ctx.Err(); err != nil {
		return sqlPath{}, err
	}
	absoluteRoot, absolute, relative, err := cleanSQLPath(root, requested)
	if err != nil {
		return sqlPath{}, err
	}
	realRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve project root: %w", err)
	}
	realAncestor, err := existingAncestor(filepath.Dir(absolute))
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve SQL directory: %w", err)
	}
	if !pathWithin(realRoot, realAncestor) {
		return sqlPath{}, errors.New("SQL path escapes the project root through a symbolic link")
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return sqlPath{}, fmt.Errorf("create SQL directory: %w", err)
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve SQL directory: %w", err)
	}
	if !pathWithin(realRoot, realParent) {
		return sqlPath{}, errors.New("SQL path escapes the project root through a symbolic link")
	}
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return sqlPath{}, fmt.Errorf("create SQL file: %w", err)
	}
	removePartial := true
	defer func() {
		_ = file.Close()
		if removePartial {
			_ = os.Remove(absolute)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return sqlPath{}, fmt.Errorf("write SQL file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return sqlPath{}, fmt.Errorf("sync SQL file: %w", err)
	}
	if err := file.Close(); err != nil {
		return sqlPath{}, fmt.Errorf("close SQL file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return sqlPath{}, err
	}
	removePartial = false
	return sqlPath{absolute: absolute, relative: filepath.ToSlash(relative)}, nil
}

func existingAncestor(value string) (string, error) {
	for {
		if _, err := os.Lstat(value); err == nil {
			return filepath.EvalSymlinks(value)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(value)
		if parent == value {
			return "", os.ErrNotExist
		}
		value = parent
	}
}

func cleanSQLPath(root, requested string) (string, string, string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", "", "", errors.New("SQL path is required")
	}
	if filepath.IsAbs(requested) || isWindowsAbsolutePath(requested) || filepath.Ext(requested) != ".sql" {
		return "", "", "", errors.New("SQL path must be a relative .sql file")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve project root: %w", err)
	}
	absolute := filepath.Clean(filepath.Join(absoluteRoot, filepath.FromSlash(requested)))
	relative, err := filepath.Rel(absoluteRoot, absolute)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve SQL path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", "", errors.New("SQL path escapes the project root")
	}
	return absoluteRoot, absolute, relative, nil
}

func pathWithin(root, value string) bool {
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func readSQLFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open SQL file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect SQL file: %w", err)
	}
	if info.IsDir() || info.Size() > maxSQLFileBytes {
		return nil, fmt.Errorf("SQL file must be at most %d bytes", maxSQLFileBytes)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxSQLFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read SQL file: %w", err)
	}
	if len(content) > maxSQLFileBytes {
		return nil, fmt.Errorf("SQL file must be at most %d bytes", maxSQLFileBytes)
	}
	return content, nil
}

func writeSQLFile(ctx context.Context, path string, content []byte) error {
	if len(content) > maxSQLFileBytes {
		return fmt.Errorf("SQL file must be at most %d bytes", maxSQLFileBytes)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect SQL file: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".sql-edit-*")
	if err != nil {
		return fmt.Errorf("create temporary SQL file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	defer temporary.Close()

	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("preserve SQL file permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write temporary SQL file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary SQL file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary SQL file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace SQL file: %w", err)
	}
	return nil
}
