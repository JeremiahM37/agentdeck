package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A similarity search always returns its best N matches. When the store holds
// little about a project, those are simply whatever else is in it — measured
// against a real Grimoire, a genuine match scored 0.37 while every unrelated
// query returned the same two facts at 0.06-0.10. Without a floor, 76 of 81
// projects were handed an identical "what this project already knows" block.
func TestRecallDropsIrrelevantMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"text": "genuinely about this project", "score": 0.37},
			{"text": "the top match in a nearly empty store", "score": 0.09},
		})
	}))
	defer srv.Close()

	g := NewGrimoire(srv.URL, "")
	facts, err := g.Recall(context.Background(), "someproject", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || !strings.Contains(facts[0].Text, "genuinely about") {
		t.Fatalf("expected only the real match, got %+v", facts)
	}
}

// A store that returns no score is taken at face value: an older Grimoire, or a
// different provider, should not be silently emptied by a floor it never opted
// into.
func TestRecallKeepsUnscoredResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{"text": "no score here"}})
	}))
	defer srv.Close()
	facts, err := NewGrimoire(srv.URL, "").Recall(context.Background(), "p", 8)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
}

func TestPrimeIsEmptyWithoutFacts(t *testing.T) {
	if Prime(nil) != "" {
		t.Fatal("no facts must add nothing to a brief")
	}
	if !strings.Contains(Prime([]Fact{{Text: "x"}}), "x") {
		t.Fatal("a fact should reach the prime")
	}
}

func TestNoneProviderIsHonestlyEmpty(t *testing.T) {
	var p Provider = None{}
	if p.Available(context.Background()) {
		t.Error("the null provider must not claim to be reachable")
	}
	facts, _ := p.Recall(context.Background(), "p", 5)
	if len(facts) != 0 {
		t.Error("the null provider must return nothing")
	}
}

// A vault holds a written note per project — that is what "what does this
// project know" means. Reading only the atomic fact store is how 76 of 81
// projects ended up with the same two sentences.
func TestRecallPrefersNotesAboutTheProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/retrieve") {
			json.NewEncoder(w).Encode([]map[string]any{
				// retrieval's best guess, but about something else entirely
				{"path": "Projects/Portfolio.md", "title": "Portfolio",
					"chunk": "a table of every project", "score": 0.9},
				// a note merely mentioning it
				{"path": "Agent Memory/project_other.md", "title": "other",
					"chunk": "we borrowed an idea from worldgen here", "score": 0.5},
				// the note named after it
				{"path": "Agent Memory/project_worldgen.md", "title": "project_worldgen",
					"chunk": "procedural dungeon plus an LLM lore track", "score": 0.4},
			})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	facts, err := NewGrimoire(srv.URL, "").Recall(context.Background(), "worldgen", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("only chunks about the project should survive: %+v", facts)
	}
	// the note named after the project outranks one that merely mentions it,
	// even though the mention scored higher
	if facts[0].Source != "Agent Memory/project_worldgen.md" {
		t.Errorf("wrong ordering: %+v", facts)
	}
	// and the source is carried, so an agent can go read the whole note
	if !strings.Contains(Prime(facts), "project_worldgen.md") {
		t.Error("the prime should cite where each piece came from")
	}
}

// Punctuation must not defeat the match: "inference-research" is
// "project_inference_research.md" in the vault.
func TestNameMatchIgnoresPunctuation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/retrieve") {
			json.NewEncoder(w).Encode([]map[string]any{
				{"path": "Agent Memory/project_inference_research_handoff.md",
					"title": "project_inference_research_handoff",
					"chunk": "paused when the quota ran out", "score": 0.3},
			})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()
	facts, _ := NewGrimoire(srv.URL, "").Recall(context.Background(), "inference-research", 8)
	if len(facts) != 1 {
		t.Fatalf("hyphens vs underscores must not defeat the match: %+v", facts)
	}
}

// Honest emptiness: a project the vault knows nothing about gets nothing.
func TestRecallReturnsNothingForAnUnknownProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/retrieve") {
			json.NewEncoder(w).Encode([]map[string]any{
				{"path": "Notes/unrelated.md", "title": "unrelated",
					"chunk": "nothing to do with it", "score": 0.8},
			})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()
	facts, _ := NewGrimoire(srv.URL, "").Recall(context.Background(), "csci-468", 8)
	if len(facts) != 0 {
		t.Fatalf("expected nothing, got %+v", facts)
	}
}
