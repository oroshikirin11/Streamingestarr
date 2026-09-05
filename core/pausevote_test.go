package core

import (
	"testing"
	"time"
)

func TestVoteThreshold(t *testing.T) {
	cases := map[int]int{0: 1, 1: 1, 2: 1, 3: 2, 4: 2, 5: 3, 10: 5, 11: 6}
	for viewers, want := range cases {
		if got := voteThreshold(viewers); got != want {
			t.Errorf("threshold(%d) = %d, want %d", viewers, got, want)
		}
	}
}

func TestVoteBoardDedupesPerUser(t *testing.T) {
	var b voteBoard
	now := time.Now()
	if !b.Cast("u1", "Alex", "pause", now) {
		t.Fatal("first vote should change the tally")
	}
	if b.Cast("u1", "Alex", "pause", now.Add(time.Second)) {
		t.Fatal("a second tab of the same user must not count again")
	}
	if b.Count(now.Add(time.Second)) != 1 {
		t.Fatalf("count = %d, want 1", b.Count(now))
	}
	b.Cast("u2", "Sam", "pause", now)
	if b.Count(now) != 2 {
		t.Fatalf("count = %d, want 2", b.Count(now))
	}
	if !b.Ready(4, now) {
		t.Fatal("2 of 4 should meet the threshold")
	}
	if b.Ready(5, now) {
		t.Fatal("2 of 5 should not meet the threshold")
	}
}

func TestVoteBoardPhase(t *testing.T) {
	var b voteBoard
	now := time.Now()
	if b.Wanted() != "pause" {
		t.Fatal("playing wants pause")
	}
	if b.Cast("u1", "Alex", "resume", now) {
		t.Fatal("a resume vote while playing must be ignored")
	}
	if b.Count(now) != 0 {
		t.Fatal("ignored vote must not count")
	}
	b.SetPaused(true, now)
	if b.Wanted() != "resume" {
		t.Fatal("paused wants resume")
	}
	if b.Cast("u1", "Alex", "pause", now) {
		t.Fatal("a pause vote while paused must be ignored")
	}
	if !b.Cast("u1", "Alex", "resume", now) {
		t.Fatal("a resume vote while paused counts")
	}
}

func TestVoteBoardExpiryAndWithdraw(t *testing.T) {
	var b voteBoard
	now := time.Now()
	b.Cast("u1", "Alex", "pause", now)
	b.Cast("u2", "Sam", "pause", now.Add(30*time.Second))
	later := now.Add(pauseVoteTTL + time.Second)
	if b.Count(later) != 1 {
		t.Fatalf("count after expiry = %d, want 1", b.Count(later))
	}
	if !b.Expire(later) {
		t.Fatal("Expire should report the dropped vote")
	}
	if _, has := b.votes["u1"]; has {
		t.Fatal("expired vote should be gone")
	}
	if !b.Withdraw("u2") {
		t.Fatal("withdraw of a standing vote reports true")
	}
	if b.Withdraw("u2") {
		t.Fatal("withdraw twice reports false")
	}
	if b.Count(later) != 0 {
		t.Fatal("nothing should stand")
	}
	// A recast after expiry counts as a change again.
	b.Cast("u1", "Alex", "pause", now)
	if !b.Cast("u1", "Alex", "pause", later) {
		t.Fatal("recasting an expired vote is a change")
	}
}

func TestVoteBoardRetainDropsUnseated(t *testing.T) {
	var b voteBoard
	now := time.Now()
	b.Cast("u1", "Alex", "pause", now)
	b.Cast("u2", "Sam", "pause", now)
	b.Retain(func(id string) bool { return id == "u2" })
	ids, names := b.Voters(now)
	if len(ids) != 1 || ids[0] != "u2" || names[0] != "Sam" {
		t.Fatalf("voters = %v %v, want only Sam", ids, names)
	}
}

func TestVoteBoardCooldownAndPending(t *testing.T) {
	var b voteBoard
	now := time.Now()
	b.Cast("u1", "Alex", "pause", now)
	if !b.Ready(1, now) {
		t.Fatal("1 of 1 is ready")
	}
	b.StartPending("pause")
	if b.Count(now) != 0 || b.pending != "pause" {
		t.Fatal("a sent command spends the tally and marks pending")
	}
	b.Cast("u1", "Alex", "pause", now)
	if b.Ready(1, now) {
		t.Fatal("nothing fires while pending")
	}
	if !b.SetPaused(true, now) {
		t.Fatal("becoming paused is a change")
	}
	if b.SetPaused(true, now) {
		t.Fatal("staying paused is not a change")
	}
	if b.pending != "" || len(b.votes) != 0 {
		t.Fatal("a state change clears pending and votes")
	}
	b.Cast("u1", "Alex", "resume", now.Add(time.Second))
	if b.Ready(1, now.Add(time.Second)) {
		t.Fatal("votes are accepted but not evaluated during cooldown")
	}
	if !b.InCooldown(now.Add(29 * time.Second)) {
		t.Fatal("still in cooldown at 29 s")
	}
	after := now.Add(pauseVoteCooldown + time.Second)
	if b.InCooldown(after) {
		t.Fatal("cooldown ends after 30 s")
	}
	if !b.Ready(1, after) {
		t.Fatal("the vote cast during cooldown counts once it ends")
	}
	b.Reset()
	if b.paused || b.pending != "" || len(b.votes) != 0 || b.InCooldown(after) {
		t.Fatal("reset forgets everything")
	}
}
