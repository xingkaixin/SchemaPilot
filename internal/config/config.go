package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

const FileName = "schemapilot.toml"

type Driver string

const (
	Postgres  Driver = "postgres"
	MySQL     Driver = "mysql"
	SQLServer Driver = "sqlserver"
	Oracle    Driver = "oracle"
	SQLite    Driver = "sqlite"
)

type Connection struct {
	Name     string            `toml:"-" json:"name"`
	Driver   Driver            `toml:"driver" json:"driver"`
	Host     string            `toml:"host,omitempty" json:"host"`
	Port     int               `toml:"port,omitempty" json:"port,omitempty"`
	Database string            `toml:"database,omitempty" json:"database"`
	User     string            `toml:"user,omitempty" json:"user"`
	Password string            `toml:"password,omitempty" json:"password"`
	Params   map[string]string `toml:"params,omitempty" json:"params,omitempty"`
}

type Config struct {
	Connections []Connection
}

type document struct {
	Connections map[string]Connection `toml:"connections"`
}

// Names double as directory names, so they must be safe path segments.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (connection Connection) Validate() error {
	if !namePattern.MatchString(connection.Name) {
		return fmt.Errorf("连接名称 %q 只能包含字母、数字、点、下划线和连字符，且以字母或数字开头", connection.Name)
	}
	switch connection.Driver {
	case Postgres, MySQL, SQLServer, Oracle:
		if connection.Host == "" {
			return errors.New("主机不能为空")
		}
	case SQLite:
		if connection.Database == "" {
			return errors.New("数据库文件不能为空")
		}
	default:
		return fmt.Errorf("不支持的数据库类型 %q", connection.Driver)
	}
	if connection.Port < 0 || connection.Port > 65535 {
		return fmt.Errorf("端口 %d 无效", connection.Port)
	}
	return nil
}

func (config Config) Find(name string) (Connection, bool) {
	for _, connection := range config.Connections {
		if connection.Name == name {
			return connection, true
		}
	}
	return Connection{}, false
}

func (config Config) Names() []string {
	names := make([]string, 0, len(config.Connections))
	for _, connection := range config.Connections {
		names = append(names, connection.Name)
	}
	return names
}

// Upsert replaces the connection called previousName (or connection.Name
// when previousName is empty) and keeps the list sorted by name.
func (config Config) Upsert(previousName string, connection Connection) (Config, error) {
	if err := connection.Validate(); err != nil {
		return config, err
	}
	if previousName == "" {
		previousName = connection.Name
	}
	next := Config{}
	for _, existing := range config.Connections {
		if existing.Name == previousName {
			continue
		}
		if existing.Name == connection.Name {
			return config, fmt.Errorf("连接 %q 已存在", connection.Name)
		}
		next.Connections = append(next.Connections, existing)
	}
	next.Connections = append(next.Connections, connection)
	sort.Slice(next.Connections, func(i, j int) bool { return next.Connections[i].Name < next.Connections[j].Name })
	return next, nil
}

func (config Config) Remove(name string) Config {
	next := Config{}
	for _, existing := range config.Connections {
		if existing.Name != name {
			next.Connections = append(next.Connections, existing)
		}
	}
	return next
}

func Path(dir string) string {
	return filepath.Join(dir, FileName)
}

// Load returns an empty config when the file does not exist.
func Load(dir string) (Config, bool, error) {
	content, err := os.ReadFile(Path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("读取 %s: %w", FileName, err)
	}
	var parsed document
	if err := toml.Unmarshal(content, &parsed); err != nil {
		return Config{}, true, fmt.Errorf("解析 %s: %w", FileName, err)
	}
	config := Config{}
	for name, connection := range parsed.Connections {
		connection.Name = name
		config.Connections = append(config.Connections, connection)
	}
	sort.Slice(config.Connections, func(i, j int) bool { return config.Connections[i].Name < config.Connections[j].Name })
	return config, true, nil
}

func Save(dir string, config Config) error {
	parsed := document{Connections: map[string]Connection{}}
	for _, connection := range config.Connections {
		parsed.Connections[connection.Name] = connection
	}
	content, err := toml.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("生成 %s: %w", FileName, err)
	}
	temporary, err := os.CreateTemp(dir, ".schemapilot-*.toml")
	if err != nil {
		return fmt.Errorf("写入 %s: %w", FileName, err)
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("写入 %s: %w", FileName, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("写入 %s: %w", FileName, err)
	}
	if err := os.Rename(temporary.Name(), Path(dir)); err != nil {
		return fmt.Errorf("写入 %s: %w", FileName, err)
	}
	return nil
}
