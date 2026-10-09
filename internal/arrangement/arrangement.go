// Package arrangement stores how each connection's files are ordered, in a
// file next to the connection config so it travels with the workspace.
package arrangement

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/schemapilot/schemapilot/internal/config"
)

const FileName = "schemapilot.arrangement.json"

// Connection is one connection's arrangement: steps run in order, the lanes
// of a step run in parallel, the files of a lane run in order.
type Connection struct {
	// Driver records what the files were arranged for, so a workspace
	// opened elsewhere can tell a connection of another kind apart.
	Driver   config.Driver `json:"driver,omitempty"`
	Steps    [][][]string  `json:"steps"`
	Disabled []string      `json:"disabled,omitempty"`
}

type Arrangement struct {
	Version     int                   `json:"version"`
	Connections map[string]Connection `json:"connections"`
	// Detached lists files moved out of their connection directory.
	Detached []string `json:"detached,omitempty"`
}

func Path(dir string) string {
	return filepath.Join(dir, FileName)
}

// Load returns the arrangement and the revision of the file it came from;
// both are empty when the file does not exist.
func Load(dir string) (*Arrangement, string, error) {
	content, err := os.ReadFile(Path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("读取 %s: %w", FileName, err)
	}
	var parsed Arrangement
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, "", fmt.Errorf("解析 %s: %w", FileName, err)
	}
	if err := parsed.Validate(); err != nil {
		return nil, "", fmt.Errorf("%s: %w", FileName, err)
	}
	return &parsed, Revision(content), nil
}

func Revision(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:8])
}

// Encode writes one step per line so that a reordering shows up as a small
// diff.
func Encode(arrangement Arrangement) ([]byte, error) {
	var buffer bytes.Buffer
	compact := func(value any) string {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	buffer.WriteString("{\n  \"version\": 1,\n  \"connections\": {")
	names := slices.Sorted(maps.Keys(arrangement.Connections))
	for index, name := range names {
		connection := arrangement.Connections[name]
		if index > 0 {
			buffer.WriteString(",")
		}
		fmt.Fprintf(&buffer, "\n    %s: {", compact(name))
		if connection.Driver != "" {
			fmt.Fprintf(&buffer, "\n      \"driver\": %s,", compact(connection.Driver))
		}
		buffer.WriteString("\n      \"steps\": [")
		for stepIndex, step := range connection.Steps {
			if stepIndex > 0 {
				buffer.WriteString(",")
			}
			buffer.WriteString("\n        " + compact(step))
		}
		if len(connection.Steps) > 0 {
			buffer.WriteString("\n      ")
		}
		buffer.WriteString("]")
		if len(connection.Disabled) > 0 {
			fmt.Fprintf(&buffer, ",\n      \"disabled\": %s", compact(connection.Disabled))
		}
		buffer.WriteString("\n    }")
	}
	if len(names) > 0 {
		buffer.WriteString("\n  ")
	}
	buffer.WriteString("}")
	if len(arrangement.Detached) > 0 {
		fmt.Fprintf(&buffer, ",\n  \"detached\": %s", compact(arrangement.Detached))
	}
	buffer.WriteString("\n}\n")
	return buffer.Bytes(), nil
}

// Save writes the arrangement atomically and returns the new revision.
func Save(dir string, arrangement Arrangement) (string, error) {
	if err := arrangement.Validate(); err != nil {
		return "", err
	}
	content, err := Encode(arrangement)
	if err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(dir, ".schemapilot-*.json")
	if err != nil {
		return "", fmt.Errorf("写入 %s: %w", FileName, err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return "", fmt.Errorf("写入 %s: %w", FileName, err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("写入 %s: %w", FileName, err)
	}
	if err := os.Rename(temporary.Name(), Path(dir)); err != nil {
		return "", fmt.Errorf("写入 %s: %w", FileName, err)
	}
	return Revision(content), nil
}

func (arrangement Arrangement) Validate() error {
	for name, connection := range arrangement.Connections {
		if !config.ValidName(name) {
			return fmt.Errorf("连接名称 %q 无效", name)
		}
		for _, step := range connection.Steps {
			for _, lane := range step {
				for _, file := range lane {
					if err := ValidatePath(file); err != nil {
						return err
					}
				}
			}
		}
		for _, file := range connection.Disabled {
			if err := ValidatePath(file); err != nil {
				return err
			}
		}
	}
	for _, file := range arrangement.Detached {
		if err := ValidatePath(file); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePath accepts workspace-relative slash paths that stay inside the
// workspace.
func ValidatePath(file string) error {
	if file == "" || path.IsAbs(file) || strings.Contains(file, "\\") || path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../") {
		return fmt.Errorf("文件路径 %q 无效", file)
	}
	return nil
}

// Files lists every file the given connections arrange, in order.
func (arrangement Arrangement) Files(names []string) []string {
	var files []string
	for _, name := range names {
		for _, step := range arrangement.Connections[name].Steps {
			for _, lane := range step {
				files = append(files, lane...)
			}
		}
	}
	return files
}
