package cli

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/cursor"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

type canceledRefreshSource struct {
	binding cache.Binding
	cancel  context.CancelFunc
}

func (s canceledRefreshSource) CacheBinding(context.Context, evidence.Request) (cache.Binding, error) {
	return s.binding, nil
}

func (s canceledRefreshSource) Observe(context.Context, evidence.Request) (evidence.Observation, error) {
	if s.cancel != nil {
		s.cancel()
		return evidence.Observation{}, context.Canceled
	}
	return evidence.Observation{}, cursor.ErrTransient
}

func (s canceledRefreshSource) Failure(err error) (cache.FailureKind, time.Time) {
	return (cursor.Adapter{}).Failure(err)
}

func TestCachedFallbackCancellationSuppressesEveryOutputFormat(t *testing.T) {
	for _, format := range []string{"compact", "json", "scalar"} {
		for _, canceled := range []bool{false, true} {
			name := format + "/transient"
			if canceled {
				name = format + "/canceled"
			}
			t.Run(name, func(t *testing.T) {
				now := fixedCLINow()
				binding := cache.Binding{Provider: "cursor", Profile: "default", ResponseBoundary: "usage", SourceKind: "native_file_http", SourceName: "cursor_cli_auth_json", CredentialFingerprint: "synthetic"}
				store := cache.New(t.TempDir(), cache.Options{Now: func() time.Time { return now }})
				observation := cacheObservation(now)
				observation.Provider = "cursor"
				observation.Source.Name = "cursor_cli_auth_json"
				observation.Account = evidence.AccountIdentity{Binding: evidence.IdentityUnknown}
				if _, err := store.Put(t.Context(), binding, observation); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				source := canceledRefreshSource{binding: binding}
				if canceled {
					source.cancel = cancel
				}
				adapter := runtimeAdapter{cursor: source, newStore: func() (*cache.Store, error) { return store, nil }}
				args := []string{"--provider=cursor", "--profile=default", "--refresh", "--stale-on-error"}
				if format == "scalar" {
					args = append([]string{"value", "--window=five_hour", "--field=remaining"}, args...)
				} else {
					args = append(args, "--format="+format)
				}
				var stdout, stderr bytes.Buffer
				code := executeWithAdapterAt(ctx, args, &stdout, &stderr, "test", now, adapter)
				if canceled {
					if code != 130 || stdout.Len() != 0 || stderr.String() != "remainder: interrupted\n" {
						t.Fatalf("canceled %s: exit=%d stdout=%q stderr=%q", format, code, stdout.String(), stderr.String())
					}
				} else if code != 0 || stdout.Len() == 0 || stderr.Len() != 0 {
					t.Fatalf("transient fallback %s: exit=%d stdout=%q stderr=%q", format, code, stdout.String(), stderr.String())
				}
			})
		}
	}
}
