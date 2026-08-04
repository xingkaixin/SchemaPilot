package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/schemapilot/schemapilot/internal/httpapi"
	"github.com/schemapilot/schemapilot/internal/webui"
)

func newServeCommand(stdout io.Writer) *cobra.Command {
	var options projectOptions
	var address string
	command := &cobra.Command{
		Use:   "serve [migration.yaml]",
		Short: "Serve the migration Web console and API",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			graphPath, databasesPath, statePath, err := options.paths(positionalGraph(args))
			if err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(stdout, nil))
			logger.InfoContext(command.Context(), "opening migration workspace",
				"graph", graphPath,
				"databases", databasesPath,
				"state", statePath,
			)
			runtime, err := openServiceRuntime(statePath)
			if err != nil {
				return err
			}
			defer runtime.store.Close()
			api, err := httpapi.New(httpapi.Config{
				Context:       command.Context(),
				Engine:        runtime.engine,
				Connector:     runtime.connector,
				GraphPath:     graphPath,
				DatabasesPath: databasesPath,
				Assets:        webui.Assets(),
				Logger:        logger,
			})
			if err != nil {
				return err
			}
			server := &http.Server{
				Addr:              address,
				Handler:           api.Handler(),
				ReadHeaderTimeout: 5 * time.Second,
				IdleTimeout:       2 * time.Minute,
			}
			fmt.Fprintf(stdout, "SchemaPilot Web console: http://%s\n", address)
			return serve(command.Context(), server)
		},
	}
	options.bind(command, true)
	command.Flags().StringVar(&address, "listen", "127.0.0.1:8080", "HTTP listen address")
	return command
}

func serve(ctx context.Context, server *http.Server) error {
	shutdownComplete := make(chan struct{})
	go func() {
		defer close(shutdownComplete)
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownComplete
		return nil
	}
	return err
}
