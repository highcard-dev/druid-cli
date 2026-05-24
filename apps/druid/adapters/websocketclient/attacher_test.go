package websocketclient

import "testing"

func TestAttacherWebsocketURLUsesDaemonURL(t *testing.T) {
	attacher := NewAttacherForTarget("", "https://daemon.example/base/")
	got, err := attacher.websocketURL("scroll-a", "start")
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://daemon.example/base/ws/v1/scrolls/scroll-a/consoles/start"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}
