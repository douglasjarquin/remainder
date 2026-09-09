package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
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
	format      string
	provider    evidence.Provider
	profile     evidence.Profile
	window      evidence.WindowID
	scope       evidence.Scope
	field       evidence.Field
	account     string
	freshness   evidence.FreshnessPolicy
	cachePolicy cache.Policy
	cacheSet    bool
	all         bool
}

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer, version string) int {
	return executeWithAdapter(ctx, args, stdout, stderr, version, defaultRuntimeAdapter())
}

func ExecuteWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, version string, adapter Adapter) int {
	return executeWithAdapterClock(ctx, args, stdout, stderr, version, utcNow, adapter)
}

func executeWithAdapter(ctx context.Context, args []string, stdout, stderr io.Writer, version string, adapter Adapter) int {
	return executeWithAdapterClock(ctx, args, stdout, stderr, version, utcNow, adapter)
}

func ExecuteWithAdapterAt(ctx context.Context, args []string, stdout, stderr io.Writer, version string, now time.Time, adapter Adapter) int {
	return executeWithAdapterAt(ctx, args, stdout, stderr, version, now, adapter)
}

func executeWithAdapterAt(ctx context.Context, args []string, stdout, stderr io.Writer, version string, now time.Time, adapter Adapter) int {
	return executeWithAdapterClock(ctx, args, stdout, stderr, version, func() time.Time { return now }, adapter)
}

func executeWithAdapterClock(ctx context.Context, args []string, stdout, stderr io.Writer, version string, now func() time.Time, adapter Adapter) int {
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
		case errors.Is(err, ErrUnavailable), errors.Is(err, cache.ErrUnavailable), errors.Is(err, cache.ErrLockTimeout), errors.Is(err, cache.ErrBackoff), errors.Is(err, context.DeadlineExceeded), errors.Is(err, evidence.ErrProviderUnavailable):
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

type flagValues struct {
	format, provider, profile, window, scope, account, freshness, cache, field string
	maxAge                                                                     time.Duration
	refresh, staleOnError, all                                                 bool
}

func newRoot(version string, stdout, stderr io.Writer, adapter Adapter, now func() time.Time) *cobra.Command {
	var values flagValues
	root := &cobra.Command{
		Use:           "remainder",
		Short:         "Read quota evidence from a supported provider",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := readOptions(cmd, values, false)
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
	root.PersistentFlags().StringVar(&values.format, "format", "compact", "output format: compact or json")
	root.PersistentFlags().StringVar(&values.provider, "provider", "", "exact provider selection")
	root.PersistentFlags().StringVar(&values.profile, "profile", "", "exact profile selection")
	root.PersistentFlags().StringVar(&values.window, "window", "", "exact window selection")
	root.PersistentFlags().StringVar(&values.scope, "scope", "", "exact scope selection")
	root.PersistentFlags().StringVar(&values.account, "account", "", "expected last-observed account")
	root.PersistentFlags().StringVar(&values.freshness, "freshness", string(evidence.FreshAny), "freshness policy: any or fresh")
	root.PersistentFlags().StringVar(&values.cache, "cache", string(cache.ModeAuto), "cache policy: auto, off, or only")
	root.PersistentFlags().DurationVar(&values.maxAge, "max-age", 5*time.Second, "maximum cache observation age")
	root.PersistentFlags().BoolVar(&values.refresh, "refresh", false, "require an observation newer than this request's starting generation")
	root.PersistentFlags().BoolVar(&values.staleOnError, "stale-on-error", false, "return stale evidence after a transient refresh failure")
	root.PersistentFlags().BoolVar(&values.all, "all", false, "read all configured sources")

	value := &cobra.Command{
		Use:   "value",
		Short: "Print one exact quota value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := readOptions(cmd, values, true)
			if err != nil {
				return err
			}
			if opts.format != "compact" {
				return fmt.Errorf("%w: value does not support format %q", ErrUsage, opts.format)
			}
			request := evidence.ValueRequest{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Field: opts.field, Account: opts.account}
			observation, err := observe(cmd, adapter, evidence.Request{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Field: opts.field, Account: opts.account, Freshness: opts.freshness}, opts.cachePolicy, opts.cacheSet)
			if err != nil {
				return err
			}
			if observation.Outcome == evidence.OutcomeUnavailable {
				return ErrUnavailable
			}
			if opts.field == evidence.FieldPace {
				observation = evidence.WithPace(observation, now())
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
	value.Flags().StringVar(&values.field, "field", "", "exact value field")
	root.AddCommand(value)
	return root
}

func readOptions(cmd *cobra.Command, values flagValues, valueCommand bool) (options, error) {
	format := values.format
	if format != "compact" && format != "json" {
		return options{}, fmt.Errorf("%w: unsupported format %q", ErrUsage, format)
	}
	provider := values.provider
	profile := values.profile
	window := values.window
	scope := values.scope
	account := values.account
	freshness := values.freshness
	if freshness != string(evidence.FreshAny) && freshness != string(evidence.FreshOnly) {
		return options{}, fmt.Errorf("%w: unsupported freshness %q", ErrUsage, freshness)
	}
	all := values.all
	field := ""
	if valueCommand {
		field = values.field
		if field == "" {
			return options{}, fmt.Errorf("%w: value requires --field", ErrUsage)
		}
		if provider == "" || profile == "" || window == "" {
			return options{}, fmt.Errorf("%w: value requires --provider, --profile, and --window", ErrUsage)
		}
		if all {
			return options{}, fmt.Errorf("%w: value cannot use --all", ErrUsage)
		}
	}
	policy := cache.Policy{Mode: cache.ModeAuto, MaxAge: 5 * time.Second}
	cacheSet := cmd.Flags().Changed("cache") || cmd.Flags().Changed("max-age") || cmd.Flags().Changed("refresh") || cmd.Flags().Changed("stale-on-error")
	if cacheSet {
		var err error
		policy, err = readCachePolicy(cmd)
		if err != nil {
			return options{}, err
		}
	}
	return options{format: format, provider: evidence.Provider(provider), profile: evidence.Profile(profile), window: evidence.WindowID(window), scope: evidence.Scope(scope), field: evidence.Field(field), account: account, freshness: evidence.FreshnessPolicy(freshness), cachePolicy: policy, cacheSet: cacheSet, all: all}, nil
}

func runReport(cmd *cobra.Command, adapter Adapter, opts options, now func() time.Time) error {
	observation, err := observe(cmd, adapter, evidence.Request{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Account: opts.account, Freshness: opts.freshness, All: opts.all}, opts.cachePolicy, opts.cacheSet)
	if err != nil {
		return err
	}
	if observation.Outcome == evidence.OutcomeUnavailable {
		return ErrUnavailable
	}
	if opts.freshness == evidence.FreshOnly && observation.Freshness != evidence.FreshFresh {
		return evidence.ErrStale
	}
	evaluatedAt := now()
	observation = evidence.WithPace(observation, evaluatedAt)
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
		output, err := evidence.RenderCompact(observation, evaluatedAt)
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
