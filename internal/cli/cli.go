package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/schemapilot/schemapilot/internal/bundle"
	"github.com/schemapilot/schemapilot/internal/config"
	"github.com/schemapilot/schemapilot/internal/httpapi"
	"github.com/schemapilot/schemapilot/internal/runner"
	"github.com/schemapilot/schemapilot/internal/webui"
	"github.com/schemapilot/schemapilot/internal/workspace"
)

var Version = "dev"

const defaultListen = "127.0.0.1:8080"

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	command := newRootCommand(stdout)
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stderr)
	return command.ExecuteContext(ctx)
}

func newRootCommand(stdout io.Writer) *cobra.Command {
	var listen string
	command := &cobra.Command{
		Use:   "schemapilot [dir | package.zip]",
		Short: "Arrange and run SQL files against your databases from a local web console",
		Long: fmt.Sprintf(`Starts a local web console for the given directory (default: current directory).

Database connections are read from and saved to %s in that directory.
.sql files directly in the directory are listed as unassigned; files under a
sub-directory named after a connection are assigned to that connection.

Given a package exported from the console, its SQL files and arrangement are
unpacked into the current directory first. Connections are matched by name
with the ones configured there; files that already exist with different
content stop the import before anything is written.`, config.FileName),
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
		RunE: func(command *cobra.Command, args []string) error {
			dir, archive := ".", ""
			if len(args) == 1 {
				if isPackage(args[0]) {
					archive = args[0]
				} else {
					dir = args[0]
				}
			}
			files, err := workspace.Open(dir)
			if err != nil {
				return err
			}
			if archive != "" {
				if err := importPackage(stdout, archive, files); err != nil {
					return err
				}
			}
			// Relative SQLite paths in the config are relative to the workspace.
			if err := os.Chdir(files.Root); err != nil {
				return err
			}
			listener, err := listenWithFallback(listen, !command.Flags().Changed("listen"))
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(command.ErrOrStderr(), nil))
			runs := runner.NewManager(command.Context(), files)
			api := httpapi.New(files, runs, webui.Assets(), logger, isLoopbackListener(listener), Version)
			server := &http.Server{Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second}

			fmt.Fprintf(stdout, "SchemaPilot\n  工作目录  %s\n  控制台    http://%s\n", files.Root, listener.Addr())
			return serve(command.Context(), server, listener)
		},
	}
	command.Flags().StringVar(&listen, "listen", defaultListen, "HTTP listen address")
	return command
}

// listenWithFallback tries the next ports when the default one is taken, so
// several directories can be served at the same time.
func listenWithFallback(address string, fallback bool) (net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	if err == nil || !fallback || !errors.Is(err, syscall.EADDRINUSE) {
		return listener, err
	}
	host, port, splitErr := net.SplitHostPort(address)
	if splitErr != nil {
		return nil, err
	}
	var base int
	fmt.Sscanf(port, "%d", &base)
	for offset := 1; offset <= 20; offset++ {
		candidate, retryErr := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(base+offset)))
		if retryErr == nil {
			return candidate, nil
		}
	}
	return nil, err
}

func isLoopbackListener(listener net.Listener) bool {
	address, ok := listener.Addr().(*net.TCPAddr)
	return ok && address.IP.IsLoopback()
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	shutdownComplete := make(chan struct{})
	go func() {
		defer close(shutdownComplete)
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownComplete
		return nil
	}
	return err
}

func isPackage(arg string) bool {
	if !strings.EqualFold(filepath.Ext(arg), ".zip") {
		return false
	}
	info, err := os.Stat(arg)
	return err == nil && info.Mode().IsRegular()
}

func importPackage(stdout io.Writer, archive string, files workspace.Workspace) error {
	summary, err := bundle.Import(archive, files)
	var conflict *bundle.ConflictError
	if errors.As(err, &conflict) {
		return fmt.Errorf("没有导入 %s：\n%w\n要使用当前目录已有的内容，直接运行 schemapilot（不带包）", filepath.Base(archive), err)
	}
	if err != nil {
		return fmt.Errorf("没有导入 %s：%w", filepath.Base(archive), err)
	}
	loaded, _, _ := config.Load(files.Root)
	var names, missing []string
	for _, connection := range summary.Manifest.Connections {
		names = append(names, connection.Name)
		if _, ok := loaded.Find(connection.Name); !ok {
			missing = append(missing, connection.Name)
		}
	}
	fmt.Fprintf(stdout, "已导入 %s（来自 %s）\n  连接      %s\n  文件      新增 %d 个，已存在 %d 个\n",
		filepath.Base(archive), summary.Manifest.Source, strings.Join(names, "、"), summary.Written, summary.Kept)
	if len(missing) > 0 {
		fmt.Fprintf(stdout, "  未配置    %s：在控制台补充连接信息后即可执行\n", strings.Join(missing, "、"))
	}
	return nil
}
