package channelrepository

import (
	"os"
	"testing"

	"streamingestarr/core/data"
)

func TestMain(m *testing.M) {
	dbFile, err := os.CreateTemp(os.TempDir(), "streamingestarr-rooms-test.db")
	if err != nil {
		panic(err)
	}
	defer os.Remove(dbFile.Name())
	if err := data.SetupPersistence(dbFile.Name()); err != nil {
		panic(err)
	}
	Setup(data.GetDatastore().DB)
	os.Exit(m.Run())
}

func TestPauseVoteSwitchPersists(t *testing.T) {
	// Default on — for the seeded room and for a new one.
	if c := GetChannel(DefaultChannelID); c == nil || !c.PauseVoteEnabled {
		t.Fatal("the default room should allow pause votes out of the box")
	}
	if err := AddChannel("annex", "Annex", "key-annex"); err != nil {
		t.Fatal(err)
	}
	if c := GetChannel("annex"); c == nil || !c.PauseVoteEnabled {
		t.Fatal("a new room should allow pause votes")
	}

	if err := SetChannelPauseVote("annex", false); err != nil {
		t.Fatal(err)
	}
	if c := GetChannel("annex"); c.PauseVoteEnabled {
		t.Fatal("switch off did not persist")
	}
	// The list reads the same column.
	for _, c := range ListChannels() {
		switch c.ID {
		case "annex":
			if c.PauseVoteEnabled {
				t.Fatal("ListChannels shows annex on")
			}
		case DefaultChannelID:
			if !c.PauseVoteEnabled {
				t.Fatal("ListChannels shows main off")
			}
		}
	}
	// The migration is idempotent: a second Setup keeps the value.
	Setup(data.GetDatastore().DB)
	if c := GetChannel("annex"); c.PauseVoteEnabled {
		t.Fatal("re-running Setup reset the switch")
	}
	if err := SetChannelPauseVote("annex", true); err != nil {
		t.Fatal(err)
	}
	if c := GetChannel("annex"); !c.PauseVoteEnabled {
		t.Fatal("switch on did not persist")
	}
}

func TestNewRoomCarriesARelayToken(t *testing.T) {

	if err := AddChannel("relay-room", "Relay Room", "key-1"); err != nil {
		t.Fatal(err)
	}
	ch := GetChannel("relay-room")
	if ch == nil || len(ch.RelayToken) < 32 {
		t.Fatalf("a new room must carry a relay token, got %q", ch.RelayToken)
	}
	rotated, err := RotateRelayToken("relay-room")
	if err != nil || rotated == ch.RelayToken || GetChannel("relay-room").RelayToken != rotated {
		t.Fatal("rotation must store a different token")
	}
}
