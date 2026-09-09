package cli

import (
	"bytes"
	"encoding/binary"
	"math"
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
	"github.com/douglasjarquin/remainder/internal/evidence"
	"github.com/douglasjarquin/remainder/internal/grok"
)

func TestExecute_native_Grok_supportsCompactJSONAndScalar(t *testing.T) {
	now := fixedCLINow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGrokCLIQuota(w, grokCLIQuotaPayload(20, 35, 7))
	}))
	defer server.Close()
	adapter := grokRuntimeAdapter(t, server, now)
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "compact", args: []string{"--provider=grok", "--profile=default", "--cache=off"}, want: `provider="grok" profile="default"`},
		{name: "JSON flattened values", args: []string{"--provider=grok", "--profile=default", "--cache=off", "--format=json"}, want: `"id":"credits_remaining","field":"remaining","state":"defined","amount":80`},
		{name: "scalar", args: []string{"value", "--provider=grok", "--profile=default", "--window=prepaid", "--field=remaining", "--cache=off"}, want: "7\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := executeWithAdapterAt(t.Context(), test.args, &stdout, &stderr, "test", now, adapter)

			if code != 0 || !strings.Contains(stdout.String(), test.want) || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecute_native_Grok_preservesZeroAndUnknownValues(t *testing.T) {
	now := fixedCLINow()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		payload := grokCLIBytesField(1, append(grokCLIFixed32Field(1, math.Float32bits(0)), grokCLIBytesField(7, grokCLIVarintField(1, 4))...))
		writeGrokCLIQuota(w, payload)
	}))
	defer server.Close()
	adapter := grokRuntimeAdapter(t, server, now)
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=grok", "--profile=default", "--cache=off", "--format=json"}, &stdout, &stderr, "test", now, adapter)

	if code != 3 || !strings.Contains(stdout.String(), `"id":"credits_used","field":"used","state":"zero","amount":0`) || !strings.Contains(stdout.String(), `"id":"product:chat_used","field":"used","state":"unknown"`) || stderr.String() != "remainder: partial evidence\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeAdapter_Grok_cacheHitPreservesTimestampAndUnknownIdentity(t *testing.T) {
	now := fixedCLINow()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeGrokCLIQuota(w, grokCLIQuotaPayload(20, 35, 7))
	}))
	defer server.Close()
	adapter := grokRuntimeAdapter(t, server, now)
	store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
	adapter.newStore = func() (*cache.Store, error) { return store, nil }
	var firstOut, secondOut, stderr bytes.Buffer
	firstCode := executeWithAdapterAt(t.Context(), []string{"--provider=grok", "--profile=default", "--format=json"}, &firstOut, &stderr, "test", now, adapter)
	secondCode := executeWithAdapterAt(t.Context(), []string{"--provider=grok", "--profile=default", "--cache=only", "--max-age=1m", "--format=json"}, &secondOut, &stderr, "test", now, adapter)

	wantTime := `"observed_at":"2026-09-09T16:00:00Z"`
	wantIdentity := `"account":{"last_observed":"","binding":"unknown"}`
	if firstCode != 0 || secondCode != 0 || !strings.Contains(firstOut.String(), wantTime) || !strings.Contains(secondOut.String(), wantTime) || !strings.Contains(secondOut.String(), wantIdentity) || requests.Load() != 1 || stderr.Len() != 0 {
		t.Fatalf("codes=%d/%d first=%q second=%q requests=%d stderr=%q", firstCode, secondCode, firstOut.String(), secondOut.String(), requests.Load(), stderr.String())
	}
}

func TestExecute_native_Grok_rejectsAccountBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writeGrokCLIQuota(w, grokCLIQuotaPayload(20, 35, 7))
	}))
	defer server.Close()
	adapter := grokRuntimeAdapter(t, server, fixedCLINow())
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=grok", "--profile=default", "--account=acct-test", "--cache=off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 2 || stdout.Len() != 0 || requests.Load() != 0 || !strings.Contains(stderr.String(), evidence.ErrWrongAccount.Error()) {
		t.Fatalf("code=%d requests=%d stdout=%q stderr=%q", code, requests.Load(), stdout.String(), stderr.String())
	}
}

func TestRuntimeAdapter_Grok_401RevokesAnd429BacksOff(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{name: "401 revokes", status: http.StatusUnauthorized},
		{name: "429 backs off", status: http.StatusTooManyRequests},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := fixedCLINow()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if test.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "30")
				}
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			adapter := grokRuntimeAdapter(t, server, now)
			store := cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }})
			adapter.newStore = func() (*cache.Store, error) { return store, nil }
			var stdout, stderr bytes.Buffer
			firstCode := executeWithAdapterAt(t.Context(), []string{"--provider=grok", "--profile=default"}, &stdout, &stderr, "test", now, adapter)
			stdout.Reset()
			stderr.Reset()
			secondArgs := []string{"--provider=grok", "--profile=default", "--cache=only", "--max-age=1m"}
			if test.status == http.StatusTooManyRequests {
				secondArgs = []string{"--provider=grok", "--profile=default"}
			}
			secondCode := executeWithAdapterAt(t.Context(), secondArgs, &stdout, &stderr, "test", now, adapter)

			if firstCode != 1 || secondCode != 1 || stdout.Len() != 0 || stderr.Len() == 0 || requests.Load() != 1 {
				t.Fatalf("codes=%d/%d stdout=%q stderr=%q requests=%d", firstCode, secondCode, stdout.String(), stderr.String(), requests.Load())
			}
		})
	}
}

func TestExecute_native_Grok_missingFileIsOperationalFailure(t *testing.T) {
	adapter := runtimeAdapter{
		codex: codex.Default(),
		grok:  grok.New(grok.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}),
		newStore: func() (*cache.Store, error) {
			t.Fatal("cache initialized")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=grok", "--profile=default", "--cache=off"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "authentication file is missing") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRuntimeAdapter_rejectsUnknownProviderBeforeCacheOrSourceIO(t *testing.T) {
	adapter := runtimeAdapter{codex: codex.New(codex.Options{AuthFile: filepath.Join(t.TempDir(), "missing")}), newStore: func() (*cache.Store, error) {
		t.Fatal("cache initialized")
		return nil, nil
	}}
	var stdout, stderr bytes.Buffer

	code := executeWithAdapterAt(t.Context(), []string{"--provider=unknown", "--profile=default", "--cache=only"}, &stdout, &stderr, "test", fixedCLINow(), adapter)

	if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unsupported provider") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func grokRuntimeAdapter(t *testing.T, server *httptest.Server, now time.Time) runtimeAdapter {
	t.Helper()
	authFile := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authFile, []byte(`{"grok.com":{"key":"synthetic-grok-session"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := grok.New(grok.Options{AuthFile: authFile, Endpoint: server.URL, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return now }})
	return runtimeAdapter{codex: codex.Default(), grok: provider, newStore: func() (*cache.Store, error) {
		return cache.New(filepath.Join(t.TempDir(), "cache"), cache.Options{Now: func() time.Time { return now }}), nil
	}}
}

func writeGrokCLIQuota(w http.ResponseWriter, payload []byte) {
	w.Header().Set("Content-Type", "application/grpc-web+proto")
	_, _ = w.Write(append(grokCLIFrame(0, payload), grokCLIFrame(0x80, []byte("grpc-status: 0\r\n"))...))
}

func grokCLIQuotaPayload(shared, product float32, prepaid uint64) []byte {
	config := grokCLIFixed32Field(1, math.Float32bits(shared))
	productPayload := append(grokCLIVarintField(1, 4), grokCLIFixed32Field(2, math.Float32bits(product))...)
	config = append(config, grokCLIBytesField(7, productPayload)...)
	config = append(config, grokCLIBytesField(12, grokCLIVarintField(1, prepaid))...)
	return grokCLIBytesField(1, config)
}

func grokCLIFrame(flags byte, payload []byte) []byte {
	result := make([]byte, 5, 5+len(payload))
	result[0] = flags
	binary.BigEndian.PutUint32(result[1:], uint32(len(payload)))
	return append(result, payload...)
}

func grokCLIBytesField(number uint64, value []byte) []byte {
	result := append(grokCLIVarint(number<<3|2), grokCLIVarint(uint64(len(value)))...)
	return append(result, value...)
}

func grokCLIFixed32Field(number uint64, value uint32) []byte {
	result := grokCLIVarint(number<<3 | 5)
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	return append(result, raw[:]...)
}

func grokCLIVarintField(number, value uint64) []byte {
	return append(grokCLIVarint(number<<3), grokCLIVarint(value)...)
}

func grokCLIVarint(value uint64) []byte {
	var result []byte
	for value >= 0x80 {
		result = append(result, byte(value)|0x80)
		value >>= 7
	}
	return append(result, byte(value))
}
