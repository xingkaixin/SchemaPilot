package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

var Version = "dev"

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	command := newRootCommand(stdout, stderr)
	command.SetArgs(args)
	return command.ExecuteContext(ctx)
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:           "schemapilot",
		Short:         "Orchestrate database migrations as a DAG",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
	}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.AddCommand(
		newValidateCommand(stdout),
		newRunCommand(stdout),
		newResumeCommand(stdout),
		newStatusCommand(stdout),
		newRunsCommand(stdout),
		newServeCommand(stdout),
	)
	command.SetFlagErrorFunc(func(command *cobra.Command, err error) error {
		return fmt.Errorf("%s: %w", command.CommandPath(), err)
	})
	return command
}
