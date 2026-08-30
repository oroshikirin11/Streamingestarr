package auth

import "testing"

func TestThrottleLocksAfterMaxAttempts(t *testing.T) {
	ip := "test-ip-1"
	defer ThrottleReset(ip)
	for i := 0; i < maxAttempts; i++ {
		if ThrottleCheck(ip) != 0 {
			t.Fatalf("locked too early at attempt %d", i)
		}
		ThrottleFail(ip)
	}
	if ThrottleCheck(ip) == 0 {
		t.Fatal("not locked after max attempts")
	}
}

func TestThrottleResetClears(t *testing.T) {
	ip := "test-ip-2"
	for i := 0; i < maxAttempts; i++ {
		ThrottleFail(ip)
	}
	ThrottleReset(ip)
	if ThrottleCheck(ip) != 0 {
		t.Fatal("reset did not clear the lock")
	}
}
