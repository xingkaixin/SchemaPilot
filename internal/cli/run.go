package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/schemapilot/schemapilot/internal/execution"
	"github.com/schemapilot/schemapilot/internal/migration"
	"github.com/schemapilot/schemapilot/internal/project"
)

func newValidateCommand(stdout io.Writer) *cobra.Command {
	var options projectOptions
	command := &cobra.Command{
		Use:   "validate [migration.yaml]",
		Short: "Validate the migration graph, profiles, and SQL files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			graphPath, databasesPath, _, err := options.paths(positionalGraph(args))
			if err != nil {
				return err
			}
			loaded, err := project.Load(command.Context(), graphPath, databasesPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Valid migration graph %q: %d nodes, %d scripts\n", loaded.Graph.Name, len(loaded.Graph.Nodes), scriptCount(loaded.Graph))
			return nil
		},
	}
	options.bind(command, false)
	return command
}

func newRunCommand(stdout io.Writer) *cobra.Command {
	var options projectOptions
	var force bool
	var nodes []string
	command := &cobra.Command{
		Use:   "run [migration.yaml]",
		Short: "Start a migration run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			graphPath, databasesPath, statePath, err := options.paths(positionalGraph(args))
			if err != nil {
				return err
			}
			runtime, err := openRuntime(command.Context(), graphPath, databasesPath, statePath)
			if err != nil {
				return err
			}
			defer runtime.store.Close()
			scoped, err := project.Scoped(runtime.project, nodes)
			if err != nil {
				return err
			}
			printer := newEventPrinter(stdout)
			handle, err := runtime.engine.Start(command.Context(), execution.StartRequest{Project: scoped, Force: force, Notify: printer.notify})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Migration started: %s\n\n", handle.ID)
			return waitAndReport(command.Context(), stdout, runtime.engine, handle)
		},
	}
	options.bind(command, true)
	command.Flags().BoolVar(&force, "force", false, "execute scripts whose applied checksum changed")
	command.Flags().StringArrayVar(&nodes, "node", nil, "run only this node and its dependencies (repeatable)")
	return command
}

func newResumeCommand(stdout io.Writer) *cobra.Command {
	var options projectOptions
	var force bool
	command := &cobra.Command{
		Use:   "resume [run-id]",
		Short: "Resume the latest failed run or a specific run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			graphPath, databasesPath, statePath, err := options.paths("")
			if err != nil {
				return err
			}
			runtime, err := openRuntime(command.Context(), graphPath, databasesPath, statePath)
			if err != nil {
				return err
			}
			defer runtime.store.Close()
			runID := execution.RunID(positionalGraph(args))
			if runID == "" {
				runID, err = runtime.engine.LatestResumable(command.Context())
				if err != nil {
					return err
				}
			}
			snapshot, err := runtime.engine.Snapshot(command.Context(), runID)
			if err != nil {
				return err
			}
			// A scoped run resumes against the same subgraph it was created with.
			scoped, err := project.Scoped(runtime.project, runNodeNames(snapshot))
			if err != nil {
				return err
			}
			printer := newEventPrinter(stdout)
			handle, err := runtime.engine.Resume(command.Context(), execution.ResumeRequest{RunID: runID, Project: scoped, Force: force, Notify: printer.notify})
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Migration resumed: %s\n\n", handle.ID)
			return waitAndReport(command.Context(), stdout, runtime.engine, handle)
		},
	}
	options.bind(command, true)
	command.Flags().BoolVar(&force, "force", false, "execute scripts whose applied checksum changed")
	return command
}

func runNodeNames(snapshot execution.RunSnapshot) []string {
	names := make([]string, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		names = append(names, node.Name)
	}
	return names
}

func waitAndReport(ctx context.Context, stdout io.Writer, engine *execution.Engine, handle execution.Handle) error {
	waitErr := handle.Wait(ctx)
	snapshot, snapshotErr := engine.Snapshot(context.WithoutCancel(ctx), handle.ID)
	if snapshotErr != nil {
		return errors.Join(waitErr, snapshotErr)
	}
	printSnapshot(stdout, snapshot)
	if waitErr != nil {
		return waitErr
	}
	if snapshot.Run.Status == migration.RunStatusCompletedWithErrors {
		return fmt.Errorf("migration run %s completed with errors", handle.ID)
	}
	return nil
}

type eventPrinter struct {
	output io.Writer
	mu     sync.Mutex
}

func newEventPrinter(output io.Writer) *eventPrinter {
	return &eventPrinter{output: output}
}

func (printer *eventPrinter) notify(event execution.Event) {
	printer.mu.Lock()
	defer printer.mu.Unlock()
	symbol := "•"
	if event.Log.Level == execution.LogLevelWarn {
		symbol = "!"
	}
	if event.Log.Level == execution.LogLevelError {
		symbol = "✗"
	}
	location := event.Log.Node
	if event.Log.Script != "" {
		location += "/" + event.Log.Script
	}
	if location != "" {
		location += ": "
	}
	fmt.Fprintf(printer.output, "%s %s%s\n", symbol, location, event.Log.Message)
}

func printSnapshot(output io.Writer, snapshot execution.RunSnapshot) {
	fmt.Fprintf(output, "\nRun: %s\nStatus: %s\nProgress: %d/%d nodes completed\n", snapshot.Run.ID, snapshot.Run.Status, completedNodes(snapshot.Nodes), len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		fmt.Fprintf(output, "%s %s\n", statusSymbol(string(node.Status)), node.Name)
		for _, script := range node.Scripts {
			fmt.Fprintf(output, "  %s %s", statusSymbol(string(script.Status)), script.Path)
			if script.FinishedAt != nil && script.StartedAt != nil {
				fmt.Fprintf(output, " (%s)", script.FinishedAt.Sub(*script.StartedAt).Round(time.Millisecond))
			}
			fmt.Fprintln(output)
		}
	}
}

func completedNodes(nodes []execution.NodeSnapshot) int {
	count := 0
	for _, node := range nodes {
		if node.Status.AllowsDownstream() {
			count++
		}
	}
	return count
}

func statusSymbol(status string) string {
	switch status {
	case "succeeded", "already_applied":
		return "✓"
	case "failed", "blocked", "cancelled":
		return "✗"
	case "completed_with_errors":
		return "!"
	case "running":
		return "↻"
	default:
		return "·"
	}
}

func scriptCount(graph migration.Graph) int {
	count := 0
	for _, node := range graph.Nodes {
		count += len(node.Scripts)
	}
	return count
}
