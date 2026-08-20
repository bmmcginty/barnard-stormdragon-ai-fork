package gumble

import (
	"testing"

	"git.stormux.org/storm/barnard/gumble/gumble/MumbleProto"
	"google.golang.org/protobuf/proto"
)

// Regression: FromServer loss counters were accidentally copied from
// FromClient, hiding the server-to-client packet-loss condition.
func TestUserStatsUsesFromServerCounters(t *testing.T) {
	c := &Client{Config: NewConfig(), Users: make(Users)}
	u := c.Users.create(7)
	session := uint32(7)
	clientLate, serverLate := uint32(1), uint32(9)
	data, _ := proto.Marshal(&MumbleProto.UserStats{Session: &session,
		FromClient: &MumbleProto.UserStats_Stats{Late: &clientLate},
		FromServer: &MumbleProto.UserStats_Stats{Late: &serverLate}})
	if err := c.handleUserStats(data); err != nil {
		t.Fatal(err)
	}
	if u.Stats.FromServer.Late != serverLate {
		t.Fatalf("got %d, want %d", u.Stats.FromServer.Late, serverLate)
	}
}
