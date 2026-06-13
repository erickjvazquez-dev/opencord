package ws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/erickjvazquez-dev/opencord/internal/chat"
)

// A presence event must serialize to just type+online — the message/history/error
// fields are omitempty so the client never sees null payloads it doesn't expect.
func TestEventJSON_PresenceOmitsEmptyFields(t *testing.T) {
	data, err := json.Marshal(Event{Type: "presence", Online: 2})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"type":"presence"`) || !strings.Contains(s, `"online":2`) {
		t.Fatalf("presence event missing expected fields: %s", s)
	}
	for _, unexpected := range []string{`"message"`, `"history"`, `"error"`} {
		if strings.Contains(s, unexpected) {
			t.Fatalf("presence event should omit %s: %s", unexpected, s)
		}
	}
}

// A message event must round-trip the embedded chat.Message intact (the field
// names are the wire contract the web client depends on).
func TestEventJSON_MessageRoundTrip(t *testing.T) {
	m := chat.Message{ID: 1, UserID: 7, Username: "dave", Body: "hi"}
	data, err := json.Marshal(Event{Type: "message", Message: &m})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round struct {
		Type    string        `json:"type"`
		Message *chat.Message `json:"message"`
	}
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Type != "message" || round.Message == nil ||
		round.Message.Username != "dave" || round.Message.Body != "hi" {
		t.Fatalf("round-trip mismatch: %s", data)
	}
}
