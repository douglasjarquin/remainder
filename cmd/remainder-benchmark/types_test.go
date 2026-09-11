package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestControlledRefreshSampleJSON(t *testing.T) {
	value := sample{Kind: "sample", Source: "compiled-test-helper-cobra-entrypoint", Workload: "controlled-refresh", ElapsedNS: 100, ControlledTLSRoundTripNS: 40, RequestCount: 1, HelperBuild: &buildMetadata{Status: "observed", Path: "github.com/douglasjarquin/remainder/internal/cli.test"}}

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, field := range []string{`"source":"compiled-test-helper-cobra-entrypoint"`, `"elapsed_ns":100`, `"controlled_tls_round_trip_ns":40`, `"request_count":1`, `"helper_build":{"status":"observed"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("JSON %q does not contain %q", text, field)
		}
	}
}

func TestControlledRefreshSampleJSON_preservesMissingMetrics(t *testing.T) {
	data, err := json.Marshal(sample{Kind: "sample", Source: "compiled-test-helper-cobra-entrypoint", HelperBuild: &buildMetadata{Status: "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"request_count":0`) || !strings.Contains(text, `"controlled_tls_round_trip_ns":0`) {
		t.Fatalf("JSON omits explicit missing metrics: %s", text)
	}
}
