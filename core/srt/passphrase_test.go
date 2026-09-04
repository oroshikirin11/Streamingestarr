package srt

import "testing"

func TestEffectivePassphrase(t *testing.T) {
	if got := effectivePassphrase("", ""); got != "" {
		t.Fatalf("no passphrase anywhere: got %q", got)
	}
	if got := effectivePassphrase("", "global-secret"); got != "global-secret" {
		t.Fatalf("global applies when the room has none: got %q", got)
	}
	if got := effectivePassphrase("room-secret", "global-secret"); got != "room-secret" {
		t.Fatalf("the room's own passphrase wins: got %q", got)
	}
	if got := effectivePassphrase("room-secret", ""); got != "room-secret" {
		t.Fatalf("a room passphrase locks the door even with no global one: got %q", got)
	}
}
