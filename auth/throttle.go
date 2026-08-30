package auth

import (
	"sort"
	"sync"
	"time"
)

// Per-IP throttle for password attempts. Two reasons, the second sharper:
// a short shared password with no lockout is guessable online, and every
// attempt runs a memory-hard hash — unauthenticated spam would allocate
// ~19 MiB a time on the same box that keeps the broadcast alive.
// Failures cost budget, successes clear it.

const (
	attemptWindow = 15 * time.Minute
	maxAttempts   = 8
	// Bounded so an attack from many addresses (an IPv6 /64 makes buckets
	// cheap) cannot grow the map without limit.
	maxTrackedIPs = 5000
)

type attemptRecord struct {
	first time.Time
	count int
}

var (
	attemptsMu sync.Mutex
	attempts   = map[string]*attemptRecord{}
)

// ThrottleCheck returns the seconds to wait before the next attempt is
// allowed, or 0 when the attempt may proceed.
func ThrottleCheck(ip string) int {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()
	rec, ok := attempts[ip]
	if !ok {
		return 0
	}
	age := time.Since(rec.first)
	if age > attemptWindow {
		delete(attempts, ip)
		return 0
	}
	if rec.count < maxAttempts {
		return 0
	}
	return int((attemptWindow - age).Seconds()) + 1
}

// ThrottleFail records a failed attempt for an IP.
func ThrottleFail(ip string) {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()
	rec, ok := attempts[ip]
	if !ok || time.Since(rec.first) > attemptWindow {
		attempts[ip] = &attemptRecord{first: time.Now(), count: 1}
	} else {
		rec.count++
	}
	if len(attempts) > maxTrackedIPs {
		pruneAttempts()
	}
}

// ThrottleReset clears an IP's failure budget after a successful login.
func ThrottleReset(ip string) {
	attemptsMu.Lock()
	defer attemptsMu.Unlock()
	delete(attempts, ip)
}

// pruneAttempts drops expired records first and, if still over budget, the
// oldest — losing a little history beats growing without bound.
// Callers must hold attemptsMu.
func pruneAttempts() {
	cutoff := time.Now().Add(-attemptWindow)
	for ip, rec := range attempts {
		if rec.first.Before(cutoff) {
			delete(attempts, ip)
		}
	}
	if len(attempts) <= maxTrackedIPs {
		return
	}
	type entry struct {
		ip    string
		first time.Time
	}
	all := make([]entry, 0, len(attempts))
	for ip, rec := range attempts {
		all = append(all, entry{ip, rec.first})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].first.Before(all[j].first) })
	for _, e := range all[:len(attempts)-maxTrackedIPs] {
		delete(attempts, e.ip)
	}
}
