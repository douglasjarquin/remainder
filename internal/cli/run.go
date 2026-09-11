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
	ErrUnavailable = errors.New("quota is unavailable; select a supported provider")
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
	format              string
	provider            evidence.Provider
	profile             evidence.Profile
	window              evidence.WindowID
	scope               evidence.Scope
	field               evidence.Field
	account             string
	freshness           evidence.FreshnessPolicy
	cachePolicy         cache.Policy
	cacheSet            bool
	allowKeychainPrompt bool
	all                 bool
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
		if unavailable, ok := errors.AsType[*mixedUnavailableError](err); ok {
			for _, failure := range unavailable.failures {
				fmt.Fprintf(stderr, "remainder: %s/%s: %s\n", failure.Provider, failure.Profile, failure.Message)
			}
			return 1
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
	refresh, staleOnError, allowKeychainPrompt, all                            bool
}

func newRoot(version string, stdout, stderr io.Writer, adapter Adapter, now func() time.Time) *cobra.Command {
	state := &struct {
		values                 flagValues
		rootVersion, valueHelp bool
		root, value            cobra.Command
	}{}
	values := &state.values
	root := &state.root
	*root = cobra.Command{
		Use:           "remainder",
		Short:         "Read quota evidence from a supported provider",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := readOptions(cmd, *values, false)
			if err != nil {
				return err
			}
			return runReport(cmd, authorizeKeychainPrompt(adapter, opts.allowKeychainPrompt), opts, now)
		},
	}
	root.Version = version
	root.SetVersionTemplate("remainder {{.Version}} (" + module + ")\n")
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.CompletionOptions.DisableDefaultCmd = true
	flags := root.PersistentFlags()
	flags.StringVar(&values.format, "format", "compact", "output format: compact, json, or toon")
	flags.StringVar(&values.provider, "provider", "", "exact provider selection")
	flags.StringVar(&values.profile, "profile", "", "exact profile selection")
	flags.StringVar(&values.window, "window", "", "exact window selection")
	flags.StringVar(&values.scope, "scope", "", "exact scope selection")
	flags.StringVar(&values.account, "account", "", "expected last-observed account")
	flags.StringVar(&values.freshness, "freshness", string(evidence.FreshAny), "freshness policy: any or fresh")
	flags.StringVar(&values.cache, "cache", string(cache.ModeAuto), "cache policy: auto, off, or only")
	flags.DurationVar(&values.maxAge, "max-age", 5*time.Second, "maximum cache observation age")
	flags.BoolVar(&values.refresh, "refresh", false, "require an observation newer than this request's starting generation")
	flags.BoolVar(&values.staleOnError, "stale-on-error", false, "return stale evidence after a transient refresh failure")
	flags.BoolVar(&values.allowKeychainPrompt, "allow-keychain-prompt", false, "allow macOS Cursor collection to prompt for Keychain access")
	flags.BoolVar(&values.all, "all", false, "read every supported provider's default profile")
	value := &state.value
	*value = cobra.Command{
		Use:   "value",
		Short: "Print one exact quota value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts, err := readOptions(cmd, *values, true)
			if err != nil {
				return err
			}
			if opts.format != "compact" {
				return fmt.Errorf("%w: value does not support format %q", ErrUsage, opts.format)
			}
			request := evidence.ValueRequest{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Field: opts.field, Account: opts.account}
			observation, err := observe(cmd, authorizeKeychainPrompt(adapter, opts.allowKeychainPrompt), evidence.Request{Provider: opts.provider, Profile: opts.profile, Window: opts.window, Scope: opts.scope, Field: opts.field, Account: opts.account, Freshness: opts.freshness}, opts.cachePolicy, opts.cacheSet)
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
	value.Flags().BoolVarP(&state.valueHelp, "help", "h", false, "help for value")
	root.AddCommand(value)
	if version != "" {
		root.Flags().BoolVarP(&state.rootVersion, "version", "v", false, "version for remainder")
	}
	return root
}

func readOptions(cmd *cobra.Command, values flagValues, valueCommand bool) (options, error) {
	format := values.format
	if format != "compact" && format != "json" && format != "toon" {
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
	if values.allowKeychainPrompt && !all && provider != "cursor" && provider != "claude" {
		return options{}, fmt.Errorf("%w: --allow-keychain-prompt requires --provider cursor, --provider claude, or --all", ErrUsage)
	}
	if valueCommand && all {
		return options{}, fmt.Errorf("%w: value cannot use --all", ErrUsage)
	}
	field := ""
	if valueCommand {
		field = values.field
		if field == "" {
			return options{}, fmt.Errorf("%w: value requires --field", ErrUsage)
		}
		if provider == "" || profile == "" || window == "" {
			return options{}, fmt.Errorf("%w: value requires --provider, --profile, and --window", ErrUsage)
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
	return options{format: format, provider: evidence.Provider(provider), profile: evidence.Profile(profile), window: evidence.WindowID(window), scope: evidence.Scope(scope), field: evidence.Field(field), account: account, freshness: evidence.FreshnessPolicy(freshness), cachePolicy: policy, cacheSet: cacheSet, allowKeychainPrompt: values.allowKeychainPrompt, all: all}, nil
}
