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
	if strings.TrimSpace(requested) == "" {
		return sqlPath{}, errors.New("SQL path is required")
	}
	if filepath.IsAbs(requested) || filepath.Ext(requested) != ".sql" {
		return sqlPath{}, errors.New("SQL path must be a relative .sql file")
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve project root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve project root: %w", err)
	}
	absolute := filepath.Clean(filepath.Join(absoluteRoot, filepath.FromSlash(requested)))
	relative, err := filepath.Rel(absoluteRoot, absolute)
	if err != nil {
		return sqlPath{}, fmt.Errorf("resolve SQL path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return sqlPath{}, errors.New("SQL path escapes the project root")
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
