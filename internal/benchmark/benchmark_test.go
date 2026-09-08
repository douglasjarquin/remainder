package benchmark

import (
	"reflect"
	"testing"
)

func TestSummarizeSeededSamples(t *testing.T) {
	got, err := Summarize([]int64{10, 20, 30, 40, 50})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	want := Summary{
		Count:        5,
		MinNS:        10,
		MaxNS:        50,
		P50NS:        30,
		P95NS:        50,
		MeanNS:       30,
		DispersionNS: 40,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Summarize() = %#v, want %#v", got, want)
	}
}

func TestSummarizeRejectsEmptySamples(t *testing.T) {
	if _, err := Summarize(nil); err != ErrNoSamples {
		t.Fatalf("Summarize(nil) error = %v, want %v", err, ErrNoSamples)
	}
}

func TestTokenCountUsesDeclaredOfflineWordTokenizer(t *testing.T) {
	if got := TokenCount(`schema=v1 provider="codex" remaining=42tokens`); got != 3 {
		t.Fatalf("TokenCount() = %d, want 3", got)
	}
}

func TestFixtureHashIsStable(t *testing.T) {
	const want = "6526de8f9bf4cdf9e3714e6ff7be4b94978509c6a44e75da4b9aad6911fc0b55"
	if got := FixtureHash(); got != want {
		t.Fatalf("FixtureHash() = %q, want %q", got, want)
	}
}
