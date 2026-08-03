package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/schemapilot/schemapilot/internal/execution"
)

func newStatusCommand(stdout io.Writer) *cobra.Command {
	var options projectOptions
	var asJSON bool
	command := &cobra.Command{
		Use:   "status [run-id]",
		Short: "Show a migration run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, _, statePath, err := options.paths("")
			if err != nil {
				return err
			}
			store, err := openStore(statePath)
			if err != nil {
				return err
			}
			defer store.Close()
			runID := execution.RunID(positionalGraph(args))
			if runID == "" {
				runs, listErr := store.Runs(command.Context(), execution.RunQuery{Limit: 1})
				if listErr != nil {
					return listErr
				}
				if len(runs) == 0 {
					return errors.New("no migration runs found")
				}
				runID = runs[0].ID
			}
			snapshot, err := store.Snapshot(command.Context(), runID)
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdout, snapshot)
			}
			printSnapshot(stdout, snapshot)
			return nil
		},
	}
	options.bind(command, true)
	command.Flags().BoolVar(&asJSON, "json", false, "write machine-readable JSON")
	return command
}

func newRunsCommand(stdout io.Writer) *cobra.Command {
	var options projectOptions
	var limit int
	var asJSON bool
	command := &cobra.Command{
		Use:   "runs",
		Short: "List recent migration runs",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if limit < 1 || limit > 200 {
				return errors.New("limit must be between 1 and 200")
			}
			_, _, statePath, err := options.paths("")
			if err != nil {
				return err
			}
			store, err := openStore(statePath)
			if err != nil {
				return err
			}
			defer store.Close()
			runs, err := store.Runs(command.Context(), execution.RunQuery{Limit: limit})
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(stdout, runs)
			}
			if len(runs) == 0 {
				fmt.Fprintln(stdout, "No migration runs found.")
				return nil
			}
			for _, run := range runs {
				fmt.Fprintf(stdout, "%s  %-22s  %d/%d  %s\n", run.ID, run.Status, run.CompletedNodes, run.TotalNodes, run.GraphName)
			}
			return nil
		},
	}
	options.bind(command, true)
	command.Flags().IntVar(&limit, "limit", 20, "maximum number of runs")
	command.Flags().BoolVar(&asJSON, "json", false, "write machine-readable JSON")
	return command
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
