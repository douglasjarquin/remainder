package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasjarquin/remainder/internal/codex"
)

func BenchmarkExecuteCodexControlledRefresh(b *testing.B) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"account_id":"acct-test","rate_limit":{"primary_window":{"used_percent":40,"limit_window_seconds":18000}}}`)
	}))
	b.Cleanup(server.Close)
	authFile := writeCLIAuth(b)
	fixedNow := time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
	adapter := codex.New(codex.Options{AuthFile: authFile, Endpoints: []string{server.URL}, Client: server.Client(), Timeout: time.Second, Now: func() time.Time { return fixedNow }})
	args := []string{"value", "--provider", "codex", "--profile", "default", "--window", "five_hour", "--field", "remaining"}
	var stdout, stderr bytes.Buffer
	code := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		stderr.Reset()
		code = ExecuteWithAdapterAt(context.Background(), args, &stdout, &stderr, "test", fixedNow, adapter)
	}
	b.StopTimer()
	b.ReportMetric(float64(requests.Load())/float64(b.N), "requests/op")
	if code != 0 || stdout.String() != "60\n" || stderr.Len() != 0 || requests.Load() != int64(b.N) {
		b.Fatalf("result: code=%d stdout=%q stderr=%q requests=%d iterations=%d", code, stdout.String(), stderr.String(), requests.Load(), b.N)
	}
}
