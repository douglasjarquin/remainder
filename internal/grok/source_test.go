package grok

import (
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestNormalizeSourceFixtures(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	reset := now.Add(24 * time.Hour)

	t.Run("missing_usage_without_a_cycle_is_unknown", func(t *testing.T) {
		payload := quotaPayload(nil, []productFixture{{kind: 6}}, 0, time.Time{}, time.Time{}, nil)
		config, _, _ := bytesAt(mustScan(t, payload), 1)
		config = append(config, bytesField(12, nil)...)
		observation, err := normalize(bytesField(1, config), now)
		if err != nil {
			t.Fatal(err)
		}
		if observation.Outcome != evidence.OutcomePartial || limit(t, observation, "product:voice", "product:voice_used").Value.State != evidence.ValueUnknown || limit(t, observation, "prepaid", "prepaid_remaining").Value.State != evidence.ValueZero {
			t.Fatalf("missing usage = %+v", observation)
		}
	})

	t.Run("shared_and_products_do_not_overlap", func(t *testing.T) {
		shared, chat := float32(20), float32(35)
		observation, err := normalize(quotaPayload(&shared, []productFixture{{kind: 4, used: &chat}}, 2, start, reset, nil), now)
		if err != nil {
			t.Fatal(err)
		}
		if got := limit(t, observation, "credits", "credits_used").Value.Amount; got == nil || got.String() != "20" {
			t.Fatalf("shared used = %v", got)
		}
		if got := limit(t, observation, "product:chat", "product:chat_used").Value.Amount; got == nil || got.String() != "35" {
			t.Fatalf("product used = %v", got)
		}
	})

	t.Run("prepaid_integer_is_exact", func(t *testing.T) {
		balance := uint64(math.MaxUint64)
		observation, err := normalize(quotaPayload(nil, nil, 0, time.Time{}, time.Time{}, &balance), now)
		if err != nil {
			t.Fatal(err)
		}
		if got := limit(t, observation, "prepaid", "prepaid_remaining").Value.Amount; got == nil || got.String() != "18446744073709551615" {
			t.Fatalf("prepaid = %v", got)
		}
	})

	for name, value := range map[string]uint32{"nan": math.Float32bits(float32(math.NaN())), "positive_infinity": math.Float32bits(float32(math.Inf(1)))} {
		t.Run(name, func(t *testing.T) {
			config := fixed32Field(1, value)
			if _, err := normalize(bytesField(1, config), now); err == nil {
				t.Fatal("normalize accepted a non-finite percentage")
			}
		})
	}

	t.Run("missing_entitlements", func(t *testing.T) {
		if _, err := normalize(bytesField(1, nil), now); err == nil {
			t.Fatal("normalize accepted an empty credits config")
		}
	})
}

func TestAuthSourceBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	used := float32(1)
	payload := quotaPayload(&used, nil, 2, now.Add(-time.Hour), now.Add(time.Hour), nil)

	t.Run("inline_auth_is_unsupported", func(t *testing.T) {
		t.Setenv("GROK_AUTH", "inline-secret")
		_, err := Default().Observe(t.Context(), request())
		if err == nil || strings.Contains(err.Error(), "inline-secret") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("auth_path_precedes_grok_home", func(t *testing.T) {
		selected := authPath(t, "grok.com", "selected", "", "", "")
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte("malformed"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GROK_AUTH_PATH", selected)
		t.Setenv("GROK_HOME", home)
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
			assertSourceRequest(t, r, "selected")
			writeQuota(w, payload)
		}, "", now)
		if _, err := adapter.Observe(t.Context(), request()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("default_home_file", func(t *testing.T) {
		home := t.TempDir()
		path := filepath.Join(home, ".grok", "auth.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		source := authPath(t, "grok.com", "home-session", "", "", "")
		body, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, r *http.Request) {
			assertSourceRequest(t, r, "home-session")
			writeQuota(w, payload)
		}, "", now)
		if _, err := adapter.Observe(t.Context(), request()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("symlink_is_rejected_without_request", func(t *testing.T) {
		target := authPath(t, "grok.com", "symlink-secret", "", "", "")
		link := filepath.Join(t.TempDir(), "auth.json")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int32
		adapter, _ := testAdapter(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) }, link, now)
		_, err := adapter.Observe(t.Context(), request())
		if err == nil || requests.Load() != 0 || strings.Contains(err.Error(), "symlink-secret") {
			t.Fatalf("error = %v, requests = %d", err, requests.Load())
		}
	})

	t.Run("fifo_is_rejected_without_blocking", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "auth.json")
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		_, err := New(Options{AuthFile: path}).Observe(t.Context(), request())
		if err == nil || time.Since(started) > time.Second {
			t.Fatalf("error = %v, elapsed = %v", err, time.Since(started))
		}
	})

	t.Run("source_is_not_written", func(t *testing.T) {
		path := authPath(t, "grok.com", "read-only-session", "", "", "")
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) { writeQuota(w, payload) }, path, now)
		if _, err := adapter.Observe(t.Context(), request()); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatalf("auth source changed: before=%v after=%v", before, after)
		}
	})

	t.Run("account_selector_is_rejected_despite_local_hint", func(t *testing.T) {
		path := authPath(t, "grok.com", "account-session", "person@example.test", "team-9", "")
		var requests atomic.Int32
		adapter, _ := testAdapter(t, func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeQuota(w, payload)
		}, path, now)
		selected := request()
		selected.Account = "team-9"
		if _, err := adapter.Observe(t.Context(), selected); !errors.Is(err, evidence.ErrWrongAccount) {
			t.Fatalf("account selection error = %v", err)
		}
		if requests.Load() != 0 {
			t.Fatalf("requests = %d", requests.Load())
		}
	})
}

func mustScan(t *testing.T, payload []byte) []wireField {
	t.Helper()
	fields, err := scanMessage(payload, 0)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}
