//go:build linux

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/cache"
	"github.com/douglasjarquin/remainder/internal/codex"
	"github.com/douglasjarquin/remainder/internal/cursor"
	"github.com/douglasjarquin/remainder/internal/evidence"
)

func TestExecute_native_Cursor_supportsQuotaAndSharedCache_onLinux(t *testing.T) {
	now := fixedCLINow()
	var requests atomic.Int32
	server := cursorCLIServer(t, &requests, nil)
	defer server.Close()
	adapter := cursorRuntimeAdapter(t, server, now)
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"value", "--provider=cursor", "--profile=default", "--window=included_usage", "--field=remaining"}, &stdout, &stderr, "test", now, adapter)
	if code != 0 || stdout.String() != "75\n" || stderr.Len() != 0 {
		t.Fatalf("live code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, test := range []struct {
		window string
		field  string
		want   string
	}{
		{window: "auto_usage", field: "remaining", want: "50\n"},
		{window: "spend_limit", field: "remaining", want: "1250\n"},
	} {
		stdout.Reset()
		code = executeWithAdapterAt(t.Context(), []string{"value", "--provider=cursor", "--profile=default", "--window=" + test.window, "--field=" + test.field, "--cache=only", "--max-age=1m"}, &stdout, &stderr, "test", now, adapter)
		if code != 0 || stdout.String() != test.want || stderr.Len() != 0 {
			t.Fatalf("cached %s/%s code=%d stdout=%q stderr=%q", test.window, test.field, code, stdout.String(), stderr.String())
		}
	}
	stdout.Reset()
	code = executeWithAdapterAt(t.Context(), []string{"--provider=cursor", "--profile=default", "--cache=only", "--max-age=1m", "--format=json"}, &stdout, &stderr, "test", now, adapter)
	if code != 0 || requests.Load() != 3 || !strings.Contains(stdout.String(), `"account":{"last_observed":"","binding":"unknown"}`) || !strings.Contains(stdout.String(), `"observed_at":"2026-09-09T16:00:00Z"`) {
		t.Fatalf("cached report code=%d requests=%d stdout=%q stderr=%q", code, requests.Load(), stdout.String(), stderr.String())
	}
}

func TestExecute_native_Cursor_rejectsSelectorsBeforeHTTP_onLinux(t *testing.T) {
	var requests atomic.Int32
	server := cursorCLIServer(t, &requests, nil)
	defer server.Close()
	adapter := cursorRuntimeAdapter(t, server, fixedCLINow())
	for _, args := range [][]string{
		{"--provider=cursor", "--profile=other", "--cache=off"},
		{"--provider=cursor", "--profile=default", "--account=acct-other", "--cache=off"},
	} {
		var stdout, stderr bytes.Buffer
		code := executeWithAdapterAt(t.Context(), args, &stdout, &stderr, "test", fixedCLINow(), adapter)
		if code != 2 || stdout.Len() != 0 || stderr.Len() == 0 || requests.Load() != 0 {
			t.Fatalf("args=%q code=%d requests=%d stdout=%q stderr=%q", args, code, requests.Load(), stdout.String(), stderr.String())
		}
	}
}

func TestExecute_native_Cursor_retainsPartialEvidenceWhenSupplementalRPCFails_onLinux(t *testing.T) {
	var requests atomic.Int32
	server := cursorCLIServer(t, &requests, func(w http.ResponseWriter, _ *http.Request, method string) bool {
		if method == "GetPlanInfo" {
			w.WriteHeader(http.StatusForbidden)
			return true
		}
		return false
	})
	defer server.Close()
	adapter := cursorRuntimeAdapter(t, server, fixedCLINow())
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=cursor", "--profile=default", "--format=json", "--cache=off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 3 || !strings.Contains(stdout.String(), `"id":"included_usage_remaining","field":"remaining","state":"defined","amount":75`) || !strings.Contains(stdout.String(), `"scope":"plan","message":"Cursor quota request was forbidden; the cause is unknown"`) || stderr.String() != "remainder: partial evidence\n" || requests.Load() != 3 {
		t.Fatalf("code=%d requests=%d stdout=%q stderr=%q", code, requests.Load(), stdout.String(), stderr.String())
	}
}

func TestRuntimeAdapter_Cursor_revocationAndBackoffSuppressRepeatedHTTP_onLinux(t *testing.T) {
	for _, test := range []struct {
		name         string
		status       int
		supplemental bool
		wantRequests int32
	}{
		{name: "supplemental 401 revokes", status: http.StatusUnauthorized, supplemental: true, wantRequests: 2},
		{name: "required 429 backs off", status: http.StatusTooManyRequests, wantRequests: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := cursorCLIServer(t, &requests, func(w http.ResponseWriter, _ *http.Request, method string) bool {
				if test.supplemental && method != "GetPlanInfo" || !test.supplemental && method != "GetCurrentPeriodUsage" {
					return false
				}
				if test.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "30")
				}
				w.WriteHeader(test.status)
				return true
			})
			defer server.Close()
			adapter := cursorRuntimeAdapter(t, server, fixedCLINow())
			var stdout, stderr bytes.Buffer
			args := []string{"--provider=cursor", "--profile=default"}

			firstCode := executeWithAdapterAt(t.Context(), args, &stdout, &stderr, "test", fixedCLINow(), adapter)
			stdout.Reset()
			stderr.Reset()
			secondArgs := args
			if test.supplemental {
				secondArgs = append(append([]string(nil), args...), "--cache=only", "--max-age=1m")
			}
			secondCode := executeWithAdapterAt(t.Context(), secondArgs, &stdout, &stderr, "test", fixedCLINow(), adapter)

			if firstCode != 1 || secondCode != 1 || requests.Load() != test.wantRequests || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("codes=%d/%d requests=%d stdout=%q stderr=%q", firstCode, secondCode, requests.Load(), stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecute_native_Cursor_cancellationDuringSupplementalRPCWritesNoQuota_onLinux(t *testing.T) {
	planStarted := make(chan struct{})
	server := cursorCLIServer(t, nil, func(_ http.ResponseWriter, r *http.Request, method string) bool {
		if method != "GetPlanInfo" {
			return false
		}
		close(planStarted)
		<-r.Context().Done()
		return true
	})
	defer server.Close()
	adapter := cursorRuntimeAdapter(t, server, fixedCLINow())
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		select {
		case <-planStarted:
			cancel()
		case <-t.Context().Done():
		}
	}()
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(ctx, []string{"--provider=cursor", "--profile=default", "--cache=off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 130 || stdout.Len() != 0 || stderr.String() != "remainder: interrupted\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteAll_CursorFailureRetainsEarlierProviders_onLinux(t *testing.T) {
	adapter := runtimeAdapter{
		codex:  blockingNativeAdapter{observation: providerObservation(fixedCLINow(), "codex")},
		claude: blockingNativeAdapter{observation: providerObservation(fixedCLINow(), "claude")},
		grok:   blockingNativeAdapter{observation: providerObservation(fixedCLINow(), "grok")},
		cursor: cursor.New(cursor.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}),
	}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--all", "--cache=off", "--format=json"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 3 || stderr.String() != "remainder: partial evidence\n" || !strings.Contains(stdout.String(), `"provider":"codex"`) || !strings.Contains(stdout.String(), `"provider":"claude"`) || !strings.Contains(stdout.String(), `"provider":"grok"`) || !strings.Contains(stdout.String(), `"provider":"cursor","profile":"default","message":"quota is unavailable"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func cursorRuntimeAdapter(t *testing.T, server *httptest.Server, now time.Time) runtimeAdapter {
	t.Helper()
	authFile := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authFile, []byte(`{"accessToken":"synthetic-cursor-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := cursor.New(cursor.Options{AuthFile: authFile, Endpoint: server.URL, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return now }})
	cacheRoot := filepath.Join(t.TempDir(), "cache")
	return runtimeAdapter{codex: codex.Default(), cursor: provider, newStore: func() (*cache.Store, error) {
		return cache.New(cacheRoot, cache.Options{Now: func() time.Time { return now }}), nil
	}}
}

func cursorCLIServer(t *testing.T, requests *atomic.Int32, override func(http.ResponseWriter, *http.Request, string) bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Errorf("read Cursor request body: %v", err)
			return
		}
		if requests != nil {
			requests.Add(1)
		}
		method := filepath.Base(r.URL.Path)
		if override != nil && override(w, r, method) {
			return
		}
		switch method {
		case "GetCurrentPeriodUsage":
			fmt.Fprint(w, `{"billingCycleStart":"2026-08-15T00:00:00Z","billingCycleEnd":"2026-09-15T00:00:00Z","planUsage":{"totalPercentUsed":25,"autoPercentUsed":50,"apiPercentUsed":0},"spendLimitUsage":{"individualLimit":2000,"individualRemaining":1250,"individualUsed":750}}`)
		case "GetPlanInfo":
			fmt.Fprint(w, `{"planInfo":{"billingCycleEnd":"2026-09-15T00:00:00Z"}}`)
		case "GetSandUsageStatus":
			fmt.Fprint(w, `{"usesPooledEnterpriseAllowance":false,"usagePercent":40,"currentPeriodStart":"2026-09-08T00:00:00Z","nextResetTimestampUtc":"2026-09-15T00:00:00Z"}`)
		default:
			t.Fatalf("unexpected Cursor RPC %q", method)
		}
	}))
}

func TestAllRequests_includeCursorLast_onLinux(t *testing.T) {
	want := []evidence.Provider{"codex", "claude", "grok", "cursor"}
	if len(allRequests) != len(want) {
		t.Fatalf("allRequests=%+v", allRequests)
	}
	for index, request := range allRequests {
		if request.Provider != want[index] || request.Profile != "default" {
			t.Fatalf("allRequests[%d]=%+v, want %s/default", index, request, want[index])
		}
	}
}
