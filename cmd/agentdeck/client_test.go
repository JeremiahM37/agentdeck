package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/console"
)

func TestAttachmentResolvesOnServerAndRejectsShellInput(t *testing.T) {
	cfg := &config.Config{}
	t.Setenv("AGENTDECK_ATTACH_HOST", "my-server")
	argv, err := attachmentCommand(cfg, []string{"session", "17"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(argv, []string{"env", "TERM=xterm-256color", "ssh", "-tt", "my-server", "/usr/local/bin/agentdeck", "--hosted-attach", "attach", "session", "17"}) {
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

func TestAgentCLIListsAndSavesRegistry(t *testing.T) {
	var methods []string
	var saved string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Method == "PUT" {
			body, _ := io.ReadAll(r.Body)
			saved = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"name":"custom","command":"runner"}]`))
	}))
	defer srv.Close()
	c := console.New(srv.URL, "")
	if _, err := agentCommand(c, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	definition := `[{"name":"custom","command":"runner","env":{"OPENAI_BASE_URL":"http://127.0.0.1:11434/v1"}}]`
	if _, err := agentCommand(c, []string{"save", definition}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, []string{"GET /api/agents", "PUT /api/agents"}) {
		t.Fatalf("requests: %v", methods)
	}
	if saved != definition {
		t.Fatalf("saved %q", saved)
	}
	if _, err := agentCommand(c, []string{"save", "not-json"}); err == nil {
		t.Fatal("accepted invalid registry JSON")
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
