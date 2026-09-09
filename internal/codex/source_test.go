package codex

import "testing"

func TestSourceReset_setUnixAcceptsLastRFC3339Second(t *testing.T) {
	var reset sourceReset
	if err := reset.setUnix(maxRFC3339Unix); err != nil {
		t.Fatalf("setUnix() error = %v", err)
	}
	if reset.Year() != 9999 || reset.Unix() != maxRFC3339Unix {
		t.Fatalf("setUnix() = %s, want final second in year 9999", reset.Time)
	}
}
