package ws

import (
	"testing"

	"github.com/erickjvazquez-dev/opencord/internal/auth"
)

// countInChannel powers the "N online" presence badge. It must count DISTINCT USERS, not
// sockets — one person with several tabs/devices (each a separate socket, now up to
// MaxConnsPerUser) is ONE online user, matching Discord. Counting sockets would inflate the
// badge, the more so since the per-user connection cap permits multiple concurrent sockets.
func TestCountInChannelCountsDistinctUsers(t *testing.T) {
	h := NewHub(nil)
	h.clients = map[*Client]bool{
		{user: auth.User{ID: 1}, channelID: 1}: true, // user 1, tab A
		{user: auth.User{ID: 1}, channelID: 1}: true, // user 1, tab B (same person)
		{user: auth.User{ID: 1}, channelID: 1}: true, // user 1, tab C
		{user: auth.User{ID: 2}, channelID: 1}: true, // user 2
		{user: auth.User{ID: 3}, channelID: 2}: true, // user 3, a DIFFERENT channel
	}
	if got := h.countInChannel(1); got != 2 {
		t.Fatalf("countInChannel(1) = %d, want 2 distinct users (three sockets of user 1 count once)", got)
	}
	if got := h.countInChannel(2); got != 1 {
		t.Fatalf("countInChannel(2) = %d, want 1", got)
	}
	if got := h.countInChannel(999); got != 0 {
		t.Fatalf("countInChannel(empty channel) = %d, want 0", got)
	}
}
