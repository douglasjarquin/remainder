package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRunCoalescedBurst_preservesRawExitCodeOnOutputMismatch(t *testing.T) {
	// Given
	binary := filepath.Join(t.TempDir(), "wrong-output")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'wrong\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	// When
	result, err := runCoalescedBurst(t.Context(), binary, nil, false, false)

	// Then
	if err == nil || len(result.Samples) != coalescingProcessCount {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	for _, sample := range result.Samples {
		if sample.ExitCode != 0 || sample.Stdout != "wrong\n" {
			t.Fatalf("sample = %+v, want raw exit 0 and wrong stdout", sample)
		}
	}
}

func TestForcedRequestCounts_rejectsTwoOverlapRequestsAndNoLaterRequest(t *testing.T) {
	// Given
	const overlapRequests, laterRequests = int64(2), int64(0)

	// When
	valid := forcedRequestCountsValid(overlapRequests, laterRequests)

	// Then
	if valid {
		t.Fatal("request gate accepted overlap=2 and later=0")
	}
}

func TestCoalescedArgs_coverSharedResponseProjections(t *testing.T) {
	// Given
	want := [][3]string{{"weekly", "remaining", "80\n"}, {"weekly", "used", "20\n"}, {"five_hour", "remaining", "60\n"}, {"five_hour", "used", "40\n"}}

	// When
	got := make([][3]string, 0, len(want))
	for index := range len(want) {
		args, output := coalescedArgs(index, false)
		got = append(got, [3]string{args[6], args[8], output})
	}

	// Then
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projections = %v, want %v", got, want)
	}
}

func TestCoalescedArgs_forceUsesSameGenerationContract(t *testing.T) {
	// Given
	base, _ := coalescedArgs(0, false)

	// When
	forced, _ := coalescedArgs(0, true)

	// Then
	if len(forced) != len(base)+1 || forced[len(forced)-1] != "--refresh" || !reflect.DeepEqual(forced[:len(base)], base) {
		t.Fatalf("forced args = %v, base = %v", forced, base)
	}
}
