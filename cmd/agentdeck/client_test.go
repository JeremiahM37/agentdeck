package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/config"
)

func TestAttachmentResolvesOnServerAndRejectsShellInput(t *testing.T) {
	cfg := &config.Config{}
	t.Setenv("AGENTDECK_ATTACH_HOST", "my-server")
	argv, err := attachmentCommand(cfg, []string{"session", "17"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(argv, []string{"ssh", "-tt", "my-server", "/usr/local/bin/agentdeck", "attach", "session", "17"}) {
		t.Fatal(argv)
	}
	for _, args := range [][]string{{"session", "17;touch bad"}, {"-c", "17"}, {"session", "0"}} {
		if _, e := attachmentCommand(cfg, args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	t.Setenv("AGENTDECK_ATTACH_HOST", "")
	t.Setenv("AGENTDECK_API", "https://remote.example")
	if _, e := attachmentCommand(cfg, []string{"session", "17"}); e == nil {
		t.Fatal("executed remote filesystem paths locally")
	}
}
func TestLocalAttachmentUsesConfiguredAPIAndToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/term/session/17/info" || r.Header.Get("Authorization") != "Bearer test" {
			t.Error("wrong request")
		}
		w.Write([]byte(`{"attach_argv":["tmux","attach","-t","safe"]}`))
	}))
	defer srv.Close()
	t.Setenv("AGENTDECK_API", srv.URL)
	t.Setenv("AGENTDECK_ATTACH_HOST", "")
	argv, e := attachmentCommand(&config.Config{AuthToken: "test"}, []string{"session", "17"})
	if e != nil {
		t.Fatal(e)
	}
	if len(argv) != 4 || argv[3] != "safe" {
		t.Fatal(argv)
	}
}
