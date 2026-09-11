package cli

import (
	"fmt"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
	"github.com/spf13/cobra"
)

func runReport(cmd *cobra.Command, adapter Adapter, opts options, now func() time.Time) error {
	if _, ok := adapter.(allAdapter); opts.all && ok {
		return runAllReport(cmd, adapter, opts, now)
	}
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
	switch opts.format {
	case "json":
		output, err := evidence.RenderJSON(observation)
		if err != nil {
			return err
		}
		if _, err := cmd.OutOrStdout().Write(append(output, '\n')); err != nil {
			return fmt.Errorf("write JSON: %w", err)
		}
	case "toon":
		output, err := evidence.RenderTOON(observation)
		if err != nil {
			return err
		}
		if _, err := cmd.OutOrStdout().Write(append(output, '\n')); err != nil {
			return fmt.Errorf("write TOON: %w", err)
		}
	default:
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
