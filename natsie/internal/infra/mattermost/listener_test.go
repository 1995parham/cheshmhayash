package mattermost

import (
	"reflect"
	"testing"
)

func TestMatchTrigger(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		trigger string
		msg     string
		want    []string
		ok      bool
	}{
		{"miss", "!natsie", "hi all", nil, false},
		{"bang plain", "!natsie", "!natsie help", []string{"help"}, true},
		{"leading space", "!natsie", "  !natsie list  ", []string{"list"}, true},
		{"multi arg", "!natsie", "!natsie show foo", []string{"show", "foo"}, true},
		{"empty after trigger", "!natsie", "!natsie", []string{}, true},
		{"trigger as substring not at start", "!natsie", "hey !natsie", nil, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			argv, ok := matchTrigger(c.trigger, c.msg)
			if ok != c.ok {
				t.Fatalf("ok=%v want %v", ok, c.ok)
			}

			if !ok {
				return
			}

			if len(argv) == 0 && len(c.want) == 0 {
				return
			}

			if !reflect.DeepEqual(argv, c.want) {
				t.Fatalf("argv=%v want %v", argv, c.want)
			}
		})
	}
}

func TestWebsocketURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://chat.example.com":     "wss://chat.example.com",
		"http://chat.example.com:8065": "ws://chat.example.com:8065",
		"wss://already.set":            "wss://already.set",
	}

	for in, want := range cases {
		if got := websocketURL(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}
