package decode

import (
	"strings"
	"testing"
)

func TestPayloadJSON(t *testing.T) {
	payload, err := Payload(strings.NewReader(`{"items":[{"value":12.5}]}`), "json")
	if err != nil {
		t.Fatalf("Payload returned error: %v", err)
	}

	root, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", payload)
	}

	items, ok := root["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want single-item array", root["items"])
	}
}

func TestPayloadXML(t *testing.T) {
	payload, err := Payload(strings.NewReader(`
<response>
  <roll>53.2</roll>
  <action>TRACK</action>
  <tracker>
    <id>1</id>
  </tracker>
  <tracker>
    <id>2</id>
  </tracker>
</response>`), "xml")
	if err != nil {
		t.Fatalf("Payload returned error: %v", err)
	}

	root, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", payload)
	}

	if got := root["roll"]; got != 53.2 {
		t.Fatalf("roll = %#v, want 53.2", got)
	}
	if got := root["action"]; got != "TRACK" {
		t.Fatalf("action = %#v, want TRACK", got)
	}

	trackers, ok := root["tracker"].([]any)
	if !ok || len(trackers) != 2 {
		t.Fatalf("tracker = %#v, want two-element array", root["tracker"])
	}
}
