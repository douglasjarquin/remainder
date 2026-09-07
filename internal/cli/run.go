package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

const module = "github.com/douglasjarquin/remainder"

var ErrUnavailable = errors.New("no provider is implemented; quota is unavailable")

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "remainder: interrupted")
		return 130
	}

	root := newRoot(version, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "remainder: interrupted")
			return 130
		}
		fmt.Fprintf(stderr, "remainder: %s\n", err)
		if errors.Is(err, ErrUnavailable) {
			return 1
		}
		return 2
	}
	return 0
}

func newRoot(version string, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "remainder",
		Short:         "Read quota evidence from a supported provider",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			return ErrUnavailable
		},
	}
	root.Version = version
	root.SetVersionTemplate("remainder {{.Version}} (" + module + ")\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.CompletionOptions.DisableDefaultCmd = true
	return root
}
