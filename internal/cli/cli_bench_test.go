package cli

import (
	"bytes"
	"context"
	"testing"
)

func BenchmarkExecuteHelp(b *testing.B) {
	var stdout, stderr bytes.Buffer
	b.ReportAllocs()
	for b.Loop() {
		stdout.Reset()
		stderr.Reset()
		Execute(context.Background(), []string{"--help"}, &stdout, &stderr, "v0.1.0")
	}
}
