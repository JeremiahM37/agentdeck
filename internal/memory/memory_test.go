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
