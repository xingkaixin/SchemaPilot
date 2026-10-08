package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/spf13/cobra"

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
		Use:   "schemapilot [dir]",
		Short: "Arrange and run SQL files against your databases from a local web console",
		Long: fmt.Sprintf(`Starts a local web console for the given directory (default: current directory).

Database connections are read from and saved to %s in that directory.
.sql files directly in the directory are listed as unassigned; files under a
sub-directory named after a connection are assigned to that connection.`, config.FileName),
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
		RunE: func(command *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			files, err := workspace.Open(dir)
			if err != nil {
				return err
			}
			listener, err := listenWithFallback(listen, !command.Flags().Changed("listen"))
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(command.ErrOrStderr(), nil))
			runs := runner.NewManager(command.Context(), files)
			api := httpapi.New(files, runs, webui.Assets(), logger, isLoopbackListener(listener))
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
