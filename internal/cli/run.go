package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
	"github.com/spf13/cobra"
)

const module = "github.com/douglasjarquin/remainder"

var (
	ErrUnavailable = errors.New("no provider is implemented; quota is unavailable")
	ErrUsage       = errors.New("invalid command usage")
	ErrPartial     = errors.New("partial evidence")
)

type Adapter interface {
	Observe(context.Context, evidence.Request) (evidence.Observation, error)
}

type unavailableAdapter struct{}

func (unavailableAdapter) Observe(ctx context.Context, _ evidence.Request) (evidence.Observation, error) {
	if err := ctx.Err(); err != nil {
		return evidence.Observation{}, err
	}
	return evidence.Observation{}, ErrUnavailable
}

type options struct {
	format    string
	provider  evidence.Provider
	profile   evidence.Profile
	window    evidence.WindowID
	scope     evidence.Scope
	field     evidence.Field
	account   string
	freshness evidence.FreshnessPolicy
	all       bool
}

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	return executeWithAdapter(ctx, args, stdout, stderr, version, unavailableAdapter{})
}

func ExecuteWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, version string, adapter Adapter) int {
	return executeWithAdapterAt(ctx, args, stdout, stderr, version, time.Now().UTC(), adapter)
}

func executeWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, version string, adapter Adapter) int {
	return executeWithAdapterAt(ctx, args, stdout, stderr, version, time.Now().UTC(), adapter)
}

func ExecuteWithAdapterAt(ctx context.Context, args []string, stdout, stderr io.Writer, version string, now time.Time, adapter Adapter) int {
	return executeWithAdapterAt(ctx, args, stdout, stderr, version, now, adapter)
}

func executeWithAdapterAt(ctx context.Context, args []string, stdout, stderr io.Writer, version string, now time.Time, adapter Adapter) int {
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "remainder: interrupted")
		return 130
	}
	root := newRoot(version, stdout, stderr, adapter, now)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "remainder: interrupted")
			return 130
		}
		fmt.Fprintf(stderr, "remainder: %s\n", err)
		switch {
		case errors.Is(err, ErrUnavailable):
			return 1
		case errors.Is(err, ErrPartial):
			return 3
		default:
			return 2
		}
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "remainder: interrupted")
		return 130
	}
	return 0
}

func newRoot(version string, stdout, stderr io.Writer, adapter Adapter, now time.Time) *cobra.Command {
	root := &cobra.Command{
		Use:           "remainder",
		Short:         "Read quota evidence from a supported provider",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := readOptions(cmd, false)
			if err != nil {
				return err
			}
			return runReport(cmd, adapter, opts, now)
		},
	}
	root.Version = version
	root.SetVersionTemplate("remainder {{.Version}} (" + module + ")\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().String("format", "compact", "output format: compact or json")
	root.PersistentFlags().String("provider", "", "exact provider selection")
	root.PersistentFlags().String("profile", "", "exact profile selection")
	root.PersistentFlags().String("window", "", "exact window selection")
	root.PersistentFlags().String("scope", "", "exact scope selection")
	root.PersistentFlags().String("account", "", "expected last-observed account")
	root.PersistentFlags().String("freshness", string(evidence.FreshAny), "freshness policy: any or fresh")
	root.PersistentFlags().Bool("all", false, "read all configured sources")

	value := &cobra.Command{
		Use:   "value",
		Short: "Print one exact quota value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := readOptions(cmd, true)
			if err != nil {
				return err
			}
			if opts.format != "compact" {
				return fmt.Errorf("%w: value does not support format %q", ErrUsage, opts.format)
			}
			request := evidence.ValueRequest{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Field: opts.field, Account: opts.account}
			observation, err := adapter.Observe(cmd.Context(), evidence.Request{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Field: opts.field, Account: opts.account, Freshness: opts.freshness})
			if err != nil {
				return err
			}
			if observation.Outcome == evidence.OutcomeUnavailable {
				return ErrUnavailable
			}
			output, err := evidence.SelectValue(observation, request, opts.freshness)
			if err != nil {
				return err
			}
			if _, err := io.WriteString(cmd.OutOrStdout(), output); err != nil {
				return fmt.Errorf("write value: %w", err)
			}
			return partialError(observation)
		},
	}
	value.Flags().String("field", "", "exact value field")
	root.AddCommand(value)
	return root
}

func readOptions(cmd *cobra.Command, valueCommand bool) (options, error) {
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return options{}, fmt.Errorf("%w: format: %v", ErrUsage, err)
	}
	if format != "compact" && format != "json" {
		return options{}, fmt.Errorf("%w: unsupported format %q", ErrUsage, format)
	}
	provider, err := cmd.Flags().GetString("provider")
	if err != nil {
		return options{}, fmt.Errorf("%w: provider: %v", ErrUsage, err)
	}
	profile, err := cmd.Flags().GetString("profile")
	if err != nil {
		return options{}, fmt.Errorf("%w: profile: %v", ErrUsage, err)
	}
	window, err := cmd.Flags().GetString("window")
	if err != nil {
		return options{}, fmt.Errorf("%w: window: %v", ErrUsage, err)
	}
	scope, err := cmd.Flags().GetString("scope")
	if err != nil {
		return options{}, fmt.Errorf("%w: scope: %v", ErrUsage, err)
	}
	account, err := cmd.Flags().GetString("account")
	if err != nil {
		return options{}, fmt.Errorf("%w: account: %v", ErrUsage, err)
	}
	freshness, err := cmd.Flags().GetString("freshness")
	if err != nil {
		return options{}, fmt.Errorf("%w: freshness: %v", ErrUsage, err)
	}
	if freshness != string(evidence.FreshAny) && freshness != string(evidence.FreshOnly) {
		return options{}, fmt.Errorf("%w: unsupported freshness %q", ErrUsage, freshness)
	}
	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return options{}, fmt.Errorf("%w: all: %v", ErrUsage, err)
	}
	field := ""
	if valueCommand {
		field, err = cmd.Flags().GetString("field")
		if err != nil || field == "" {
			return options{}, fmt.Errorf("%w: value requires --field", ErrUsage)
		}
		if provider == "" || profile == "" || window == "" {
			return options{}, fmt.Errorf("%w: value requires --provider, --profile, and --window", ErrUsage)
		}
		if all {
			return options{}, fmt.Errorf("%w: value cannot use --all", ErrUsage)
		}
	}
	return options{format: format, provider: evidence.Provider(provider), profile: evidence.Profile(profile), window: evidence.WindowID(window), scope: evidence.Scope(scope), field: evidence.Field(field), account: account, freshness: evidence.FreshnessPolicy(freshness), all: all}, nil
}

func runReport(cmd *cobra.Command, adapter Adapter, opts options, now time.Time) error {
	observation, err := adapter.Observe(cmd.Context(), evidence.Request{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Account: opts.account, Freshness: opts.freshness, All: opts.all})
	if err != nil {
		return err
	}
	if observation.Outcome == evidence.OutcomeUnavailable {
		return ErrUnavailable
	}
	if opts.freshness == evidence.FreshOnly && observation.Freshness != evidence.FreshFresh {
		return evidence.ErrStale
	}
	observation, err = observation.ForRequest(evidence.Request{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Account: opts.account, All: opts.all})
	if err != nil {
		return err
	}
	if opts.format == "json" {
		output, err := evidence.RenderJSON(observation)
		if err != nil {
			return err
		}
		if _, err := cmd.OutOrStdout().Write(append(output, '\n')); err != nil {
			return fmt.Errorf("write JSON: %w", err)
		}
	} else {
		output, err := evidence.RenderCompact(observation, now)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), output); err != nil {
			return fmt.Errorf("write compact output: %w", err)
		}
	}
	return partialError(observation)
}

func partialError(observation evidence.Observation) error {
	if observation.Outcome == evidence.OutcomePartial {
		return ErrPartial
	}
	return nil
}
