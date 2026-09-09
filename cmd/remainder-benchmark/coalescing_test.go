package main

import (
	"reflect"
	"testing"
)

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
