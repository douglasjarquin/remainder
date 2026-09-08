package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

var ErrNoSamples = errors.New("benchmark requires at least one sample")

const FixtureSeed = "remainder-issue-4-codex-cli-fixture-v1"

type Summary struct {
	Count        int   `json:"count"`
	MinNS        int64 `json:"min_ns"`
	MaxNS        int64 `json:"max_ns"`
	P50NS        int64 `json:"p50_ns"`
	P95NS        int64 `json:"p95_ns"`
	MeanNS       int64 `json:"mean_ns"`
	DispersionNS int64 `json:"dispersion_ns"`
}

func Summarize(samples []int64) (Summary, error) {
	if len(samples) == 0 {
		return Summary{}, ErrNoSamples
	}
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total int64
	for _, sample := range sorted {
		total += sample
	}
	p95 := (95*len(sorted)+99)/100 - 1
	return Summary{
		Count:        len(sorted),
		MinNS:        sorted[0],
		MaxNS:        sorted[len(sorted)-1],
		P50NS:        sorted[(len(sorted)-1)/2],
		P95NS:        sorted[p95],
		MeanNS:       total / int64(len(sorted)),
		DispersionNS: sorted[len(sorted)-1] - sorted[0],
	}, nil
}

func TokenCount(output string) int {
	return len(strings.Fields(output))
}

func FixtureHash() string {
	sum := sha256.Sum256([]byte(FixtureSeed))
	return hex.EncodeToString(sum[:])
}
