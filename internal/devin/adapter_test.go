package devin

import (
	"context"
	json "encoding/json/v2"
	"errors"
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
	"github.com/douglasjarquin/remainder/internal/evidence"
)

var testNow = time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)

func testRequest() evidence.Request {
	return evidence.Request{Provider: "devin", Profile: "default"}
}

func writeCredentials(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testAdapter(t *testing.T, authFile string, server *httptest.Server) Adapter {
	t.Helper()
	return New(Options{AuthFile: authFile, Endpoint: server.URL, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return testNow }})
}

func fullPayload() string {
	return `{"userStatus":{"userId":"user-test","email":"user@example.com","teamId":"devin-team$account-test","teamsTier":"TEAMS_TIER_DEVIN_PRO","planStatus":{"planStart":"2026-09-17T13:57:49Z","planEnd":"2026-10-17T13:57:49Z","planInfo":{"planName":"Pro","billingStrategy":"BILLING_STRATEGY_QUOTA","monthlyPromptCredits":-1},"dailyQuotaRemainingPercent":80,"dailyQuotaResetAtUnix":"1790064000","weeklyQuotaRemainingPercent":60,"weeklyQuotaResetAtUnix":"1790496000","availablePromptCredits":-1,"acuConsumed":"12.5","acuLimit":"50"}},"planInfo":{"planName":"Pro"}}`
}

func TestObserve_fullResponseNormalizesAllWindows(t *testing.T) {
	var sawPath, sawAuth atomic.Value
	var sawMetadata atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		sawPath.Store(request.URL.Path)
		sawAuth.Store(request.Header.Get("Connect-Protocol-Version"))
		var body struct {
			Metadata map[string]string `json:"metadata"`
		}
		if err := jsonUnmarshalRequest(request, &body); err != nil {
			t.Errorf("request body: %v", err)
			return
		}
		if body.Metadata["apiKey"] != "synthetic-devin-key" || body.Metadata["ideName"] == "" {
			t.Errorf("metadata=%+v", body.Metadata)
			return
		}
		sawMetadata.Store(true)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, fullPayload())
	}))
	defer server.Close()
	authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n"+`api_server_url = "https://server.codeium.com"`+"\n")
	observation, err := testAdapter(t, authFile, server).Observe(t.Context(), testRequest())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if sawPath.Load() != statusPath || sawAuth.Load() != "1" || !sawMetadata.Load() {
		t.Fatalf("path=%v connect=%v metadata=%v", sawPath.Load(), sawAuth.Load(), sawMetadata.Load())
	}
	if observation.Provider != "devin" || observation.Profile != "default" || observation.Outcome != evidence.OutcomeComplete || observation.Freshness != evidence.FreshFresh || !observation.ObservedAt.Equal(testNow) {
		t.Fatalf("observation=%+v", observation)
	}
	if observation.Account.LastObserved != "user-test" || observation.Account.Binding != evidence.IdentityVerified {
		t.Fatalf("account=%+v", observation.Account)
	}
	if observation.Source != (evidence.SourceIdentity{Kind: "native_file_http", Name: "devin_credentials_toml"}) {
		t.Fatalf("source=%+v", observation.Source)
	}
	daily := findWindow(t, observation, "daily")
	assertLimit(t, daily, "daily_remaining", "80")
	assertLimit(t, daily, "daily_used", "20")
	assertReset(t, daily, "daily_reset", time.Unix(1790064000, 0).UTC())
	assertDuration(t, daily, "daily_duration", 24*time.Hour)
	weekly := findWindow(t, observation, "weekly")
	assertLimit(t, weekly, "weekly_remaining", "60")
	assertReset(t, weekly, "weekly_reset", time.Unix(1790496000, 0).UTC())
	assertDuration(t, weekly, "weekly_duration", 7*24*time.Hour)
	acu := findWindow(t, observation, "acu")
	if acu.Unit != "acu" {
		t.Fatalf("acu unit=%q", acu.Unit)
	}
	assertLimit(t, acu, "acu_used", "12.5")
	assertLimit(t, acu, "acu_remaining", "37.5")
	assertReset(t, acu, "acu_reset", time.Date(2026, time.October, 17, 13, 57, 49, 0, time.UTC))
	assertDuration(t, acu, "acu_duration", 30*24*time.Hour)
	credits := findWindow(t, observation, "prompt_credits")
	assertState(t, credits, "prompt_credits_remaining", evidence.ValueUnlimited)
	assertState(t, credits, "prompt_credits_used", evidence.ValueNotApplicable)
	assertState(t, credits, "prompt_credits_limit", evidence.ValueUnlimited)
}

func TestObserve_partialAndBoundedCreditWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"userStatus":{"userId":"user-test","planStatus":{"dailyQuotaRemainingPercent":100,"dailyQuotaResetAtUnix":"1790064000","weeklyQuotaRemainingPercent":40,"availablePromptCredits":"250","usedPromptCredits":"750","availableFlowCredits":0,"usedFlowCredits":2000,"availableFlexCredits":-1,"planInfo":{"monthlyPromptCredits":1000,"monthlyFlowCredits":2000}}}}`)
	}))
	defer server.Close()
	authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
	observation, err := testAdapter(t, authFile, server).Observe(t.Context(), testRequest())
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observation.Outcome != evidence.OutcomePartial {
		t.Fatalf("outcome=%q", observation.Outcome)
	}
	weekly := findWindow(t, observation, "weekly")
	assertLimit(t, weekly, "weekly_remaining", "40")
	if len(weekly.Limits) != 3 {
		t.Fatalf("weekly limits=%+v", weekly.Limits)
	}
	prompt := findWindow(t, observation, "prompt_credits")
	assertLimit(t, prompt, "prompt_credits_used", "750")
	assertLimit(t, prompt, "prompt_credits_remaining", "250")
	assertLimit(t, prompt, "prompt_credits_limit", "1000")
	flow := findWindow(t, observation, "flow_credits")
	assertState(t, flow, "flow_credits_remaining", evidence.ValueZero)
	assertLimit(t, flow, "flow_credits_used", "2000")
	flex := findWindow(t, observation, "flex_credits")
	assertState(t, flex, "flex_credits_remaining", evidence.ValueUnlimited)
	assertState(t, flex, "flex_credits_used", evidence.ValueNotApplicable)
	if len(flex.Limits) != 2 {
		t.Fatalf("flex limits=%+v", flex.Limits)
	}
}

func TestObserve_missingAndMalformedResponsesFail(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		want    string
	}{
		{name: "no user status", payload: `{}`, want: "no user status"},
		{name: "no plan status", payload: `{"userStatus":{"userId":"user-test"}}`, want: "no plan status"},
		{name: "no quota evidence", payload: `{"userStatus":{"userId":"user-test","planStatus":{"planName":"x"}}}`, want: "no quota evidence"},
		{name: "percent out of range", payload: `{"userStatus":{"userId":"user-test","planStatus":{"dailyQuotaRemainingPercent":140}}}`, want: "outside 0 through 100"},
		{name: "negative percent", payload: `{"userStatus":{"userId":"user-test","planStatus":{"weeklyQuotaRemainingPercent":-5}}}`, want: "outside 0 through 100"},
		{name: "malformed JSON", payload: `{`, want: "malformed"},
		{name: "malformed reset", payload: `{"userStatus":{"userId":"user-test","planStatus":{"dailyQuotaResetAtUnix":"not-a-time"}}}`, want: "malformed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, test.payload)
			}))
			defer server.Close()
			authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
			_, err := testAdapter(t, authFile, server).Observe(t.Context(), testRequest())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v want %q", err, test.want)
			}
		})
	}
}

func TestObserve_accountSelection(t *testing.T) {
	for _, test := range []struct {
		name        string
		userID      string
		account     string
		wantErr     error
		wantBinding evidence.IdentityBinding
	}{
		{name: "matching account is verified", userID: "user-test", account: "user-test", wantBinding: evidence.IdentityVerified},
		{name: "no selector keeps verified identity", userID: "user-test", wantBinding: evidence.IdentityVerified},
		{name: "mismatched account fails", userID: "user-test", account: "user-other", wantErr: ErrAccountMismatch},
		{name: "unverifiable account selector fails", account: "user-test", wantErr: evidence.ErrWrongAccount},
		{name: "absent identity stays unknown", wantBinding: evidence.IdentityUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				userID := ""
				if test.userID != "" {
					userID = `"userId":"` + test.userID + `",`
				}
				fmt.Fprintf(w, `{"userStatus":{%s"planStatus":{"dailyQuotaRemainingPercent":50,"dailyQuotaResetAtUnix":"1790064000"}}}`, userID)
			}))
			defer server.Close()
			authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
			request := testRequest()
			request.Account = test.account
			observation, err := testAdapter(t, authFile, server).Observe(t.Context(), request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("err=%v want %v", err, test.wantErr)
			}
			if test.wantErr == nil && observation.Account.Binding != test.wantBinding {
				t.Fatalf("binding=%q", observation.Account.Binding)
			}
		})
	}
}

func TestObserve_statusAndConnectFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		want    error
		retryAt bool
	}{
		{name: "401 revokes", status: http.StatusUnauthorized, want: ErrAuthorizationRejected},
		{name: "403 revokes", status: http.StatusForbidden, want: ErrAuthorizationRejected},
		{name: "unauthenticated revokes", status: http.StatusBadRequest, body: `{"code":"unauthenticated","message":"bad key"}`, want: ErrAuthorizationRejected},
		{name: "invalid_argument revokes", status: http.StatusBadRequest, body: `{"code":"invalid_argument","message":"metadata rejected"}`, want: ErrAuthorizationRejected},
		{name: "429 backs off", status: http.StatusTooManyRequests, want: ErrTransient, retryAt: true},
		{name: "resource_exhausted backs off", status: http.StatusBadRequest, body: `{"code":"resource_exhausted"}`, want: ErrTransient},
		{name: "unavailable is transient", status: http.StatusBadRequest, body: `{"code":"unavailable"}`, want: ErrTransient},
		{name: "500 is transient", status: http.StatusInternalServerError, want: ErrTransient},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
			_, err := testAdapter(t, authFile, server).Observe(t.Context(), testRequest())
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v want %v", err, test.want)
			}
			if test.retryAt {
				var retry *RetryError
				if !errors.As(err, &retry) || retry.RetryAt.IsZero() {
					t.Fatalf("err=%v has no retry-at", err)
				}
			}
		})
	}
}

func TestObserve_oversizedResponseFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(maxResponseBytes+1))
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
	if _, err := testAdapter(t, authFile, server).Observe(t.Context(), testRequest()); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err=%v", err)
	}
}

func TestObserve_cancellationStopsBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := testAdapter(t, authFile, server).Observe(ctx, testRequest())
	if !errors.Is(err, context.Canceled) || requests.Load() != 0 {
		t.Fatalf("err=%v requests=%d", err, requests.Load())
	}
}

func TestObserve_credentialFileBoundaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "missing file", path: filepath.Join(t.TempDir(), "missing.toml"), want: "credential file is missing"},
		{name: "directory", path: t.TempDir(), want: "not a bounded regular file"},
		{name: "empty", path: writeCredentials(t, ""), want: "no API key"},
		{name: "no api key", path: writeCredentials(t, `api_server_url = "https://server.codeium.com"`+"\n"), want: "no API key"},
		{name: "malformed", path: writeCredentials(t, "not toml at all\n"), want: "malformed"},
		{name: "bad api server", path: writeCredentials(t, `windsurf_api_key = "k"`+"\n"+`api_server_url = "http://insecure"`+"\n"), want: "API server URL is invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := testAdapter(t, test.path, server).Observe(t.Context(), testRequest())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v want %q", err, test.want)
			}
		})
	}
}

func TestObserve_symlinkCredentialFileFails(t *testing.T) {
	target := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
	link := filepath.Join(t.TempDir(), "credentials.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := New(Options{AuthFile: link, Endpoint: "http://127.0.0.1:1", Timeout: time.Second, Now: func() time.Time { return testNow }}).Observe(t.Context(), testRequest())
	if err == nil || !strings.Contains(err.Error(), "not a bounded regular file") {
		t.Fatalf("err=%v", err)
	}
}

func TestCacheBinding_bindsCredentialFileWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
	adapter := testAdapter(t, authFile, server)
	binding, err := adapter.CacheBinding(t.Context(), testRequest())
	if err != nil {
		t.Fatalf("CacheBinding: %v", err)
	}
	if binding.Provider != "devin" || binding.Profile != "default" || binding.SourceKind != "native_file_http" || binding.SourceName != "devin_credentials_toml" || binding.ResponseBoundary != "user_status" || binding.CredentialFingerprint == "" || requests.Load() != 0 {
		t.Fatalf("binding=%+v requests=%d", binding, requests.Load())
	}
	otherFile := writeCredentials(t, `windsurf_api_key = "other-key"`+"\n")
	other, err := testAdapter(t, otherFile, server).CacheBinding(t.Context(), testRequest())
	if err != nil {
		t.Fatalf("CacheBinding: %v", err)
	}
	if other.CredentialFingerprint == binding.CredentialFingerprint {
		t.Fatal("distinct credential files share a fingerprint")
	}
}

func TestObserve_rejectsInvalidSelectionBeforeIO(t *testing.T) {
	authFile := writeCredentials(t, `windsurf_api_key = "synthetic-devin-key"`+"\n")
	for _, request := range []evidence.Request{
		{Provider: "codex", Profile: "default"},
		{Provider: "devin", Profile: "other"},
		{Provider: "devin", Profile: "default", All: true},
	} {
		if _, err := New(Options{AuthFile: authFile}).Observe(t.Context(), request); !errors.Is(err, evidence.ErrInvalidSelection) {
			t.Fatalf("request=%+v err=%v", request, err)
		}
	}
}

func TestReadCredentials_parsesAPIHost(t *testing.T) {
	credential, err := readCredentials(writeCredentials(t, `windsurf_api_key = "k"`+"\n"+`api_server_url = "https://server.codeium.com"`+"\n"))
	if err != nil || credential.apiKey != "k" || credential.apiHost != "https://server.codeium.com" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
	credential, err = readCredentials(writeCredentials(t, `windsurf_api_key = "k"`+"\n"))
	if err != nil || credential.apiHost != "" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
}

func TestResolveAuthFile_envAndDefaults(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit.toml")
	if path, err := resolveAuthFile(explicit); err != nil || path != explicit {
		t.Fatalf("explicit path=%q err=%v", path, err)
	}
	t.Setenv("DEVIN_CREDENTIALS", explicit)
	if path, err := resolveAuthFile(""); err != nil || path != explicit {
		t.Fatalf("env path=%q err=%v", path, err)
	}
	t.Setenv("DEVIN_CREDENTIALS", "")
	if _, err := resolveAuthFile(""); err == nil || !strings.Contains(err.Error(), "explicitly empty") {
		t.Fatalf("err=%v", err)
	}
	t.Setenv("DEVIN_CREDENTIALS", explicit)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if path, err := resolveAuthFile(""); err != nil || path != explicit {
		t.Fatalf("explicit env wins path=%q err=%v", path, err)
	}
	os.Unsetenv("DEVIN_CREDENTIALS")
	if path, err := resolveAuthFile(""); err != nil || filepath.Base(path) != "credentials.toml" || !strings.Contains(path, filepath.Join("devin", "credentials.toml")) {
		t.Fatalf("xdg path=%q err=%v", path, err)
	}
}

func TestParseCredentials_tomlSubset(t *testing.T) {
	entries, err := parseCredentials([]byte(`
# comment
windsurf_api_key = "key-1"   # trailing
api_server_url = 'https://server.codeium.com'
count = 3
enabled = true
[section]
windsurf_api_key = "sectioned"
bare = plain
`))
	if err != nil {
		t.Fatalf("parseCredentials: %v", err)
	}
	if entries["windsurf_api_key"] != "key-1" || entries["api_server_url"] != "https://server.codeium.com" || len(entries) != 2 {
		t.Fatalf("entries=%+v", entries)
	}
	for _, body := range []string{
		`windsurf_api_key = "unterminated`,
		`windsurf_api_key = "a" extra`,
		`windsurf_api_key = "a"` + "\n" + `windsurf_api_key = "b"`,
		`["open section`,
		`"quoted" = "x"`,
	} {
		if _, err := parseCredentials([]byte(body)); err == nil {
			t.Fatalf("body=%q accepted", body)
		}
	}
}

func TestFailure_classification(t *testing.T) {
	adapter := Adapter{}
	for _, test := range []struct {
		err  error
		want cache.FailureKind
	}{
		{nil, cache.FailureNone},
		{ErrAuthorizationRejected, cache.FailureRevoked},
		{fmt.Errorf("%w: wrap", ErrAuthorizationRejected), cache.FailureRevoked},
		{ErrAccountMismatch, cache.FailureAccountMismatch},
		{evidence.ErrWrongAccount, cache.FailureAccountMismatch},
		{ErrTransient, cache.FailureTransient},
		{&RetryError{RetryAt: testNow}, cache.FailureTransient},
		{errors.New("other"), cache.FailurePermanent},
	} {
		kind, retryAt := adapter.Failure(test.err)
		if kind != test.want {
			t.Fatalf("err=%v kind=%v want %v", test.err, kind, test.want)
		}
		if _, ok := test.err.(*RetryError); ok && !retryAt.Equal(testNow) {
			t.Fatalf("retryAt=%v", retryAt)
		}
	}
}

func jsonUnmarshalRequest(request *http.Request, target any) error {
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

func findWindow(t *testing.T, observation evidence.Observation, id string) evidence.Window {
	t.Helper()
	for _, window := range observation.Windows {
		if string(window.ID) == id {
			return window
		}
	}
	t.Fatalf("window %s not found in %+v", id, observation.Windows)
	return evidence.Window{}
}

func findLimit(t *testing.T, window evidence.Window, id string) evidence.Limit {
	t.Helper()
	for _, limit := range window.Limits {
		if limit.ID == id {
			return limit
		}
	}
	t.Fatalf("limit %s not found in %+v", id, window.Limits)
	return evidence.Limit{}
}

func assertLimit(t *testing.T, window evidence.Window, id, want string) {
	t.Helper()
	limit := findLimit(t, window, id)
	if limit.Value.State != evidence.ValueDefined && limit.Value.State != evidence.ValueZero || limit.Value.Amount == nil || string(*limit.Value.Amount) != want {
		t.Fatalf("limit %s value=%+v want %s", id, limit.Value, want)
	}
}

func assertState(t *testing.T, window evidence.Window, id string, state evidence.ValueState) {
	t.Helper()
	limit := findLimit(t, window, id)
	if limit.Value.State != state {
		t.Fatalf("limit %s state=%q want %q", id, limit.Value.State, state)
	}
}

func assertReset(t *testing.T, window evidence.Window, id string, want time.Time) {
	t.Helper()
	limit := findLimit(t, window, id)
	if limit.ResetAt == nil || !limit.ResetAt.Equal(want) {
		t.Fatalf("limit %s reset=%v want %v", id, limit.ResetAt, want)
	}
}

func assertDuration(t *testing.T, window evidence.Window, id string, want time.Duration) {
	t.Helper()
	limit := findLimit(t, window, id)
	if limit.Duration == nil || *limit.Duration != want {
		t.Fatalf("limit %s duration=%v want %v", id, limit.Duration, want)
	}
}
