package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/grok"
)

func BenchmarkExecuteGrokControlledRefresh(b *testing.B) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("Content-Type") != "application/grpc-web+proto" || r.Header.Get("Origin") != "https://grok.com" || r.Header.Get("Referer") != "https://grok.com/?_s=usage" || r.Header.Get("X-Grpc-Web") != "1" || r.Header.Get("X-User-Agent") != "connect-es/2.1.1" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/grpc-web+proto")
		_, _ = w.Write(grokControlledBenchmarkResponse)
	}))
	b.Cleanup(server.Close)
	authPath := filepath.Join(b.TempDir(), "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"grok.com":{"key":"synthetic-secret"}}`), 0o600); err != nil {
		b.Fatal(err)
	}
	fixedNow := time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
	adapter := grok.New(grok.Options{AuthFile: authPath, Endpoint: server.URL, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return fixedNow }})
	args := []string{"value", "--provider", "grok", "--profile", "default", "--window", "credits", "--field", "remaining"}
	var stdout, stderr bytes.Buffer
	code := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		stderr.Reset()
		code = ExecuteWithAdapterAt(b.Context(), args, &stdout, &stderr, "test", fixedNow, adapter)
	}
	b.StopTimer()
	b.ReportMetric(float64(requests.Load())/float64(b.N), "requests/op")
	if code != 0 || stdout.String() != "60\n" || stderr.Len() != 0 || requests.Load() != int64(b.N) {
		b.Fatalf("result: code=%d stdout=%q stderr=%q requests=%d iterations=%d", code, stdout.String(), stderr.String(), requests.Load(), b.N)
	}
}

var grokControlledBenchmarkResponse = []byte{0, 0, 0, 0, 7, 10, 5, 13, 0, 0, 32, 66, 128, 0, 0, 0, 16, 'g', 'r', 'p', 'c', '-', 's', 't', 'a', 't', 'u', 's', ':', ' ', '0', '\r', '\n'}
