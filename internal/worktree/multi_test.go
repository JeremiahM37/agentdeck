package worktree

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiWorkspacePlansSeparateOwnedRepositoriesAndBases(t *testing.T) {
	sources := []RepositorySource{{Name: "../API", Repo: "/repos/backend", Base: "main"}, {Name: "API", Repo: "/repos/frontend", Base: "develop"}, {Name: "資料", Repo: "/repos/library"}}
	p, err := PlanMultiWorkspace(sources, 8, InteractiveOptions{Branch: "feature/shared"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]bool{p.Token: true}
	paths := map[string]bool{}
	for i, r := range p.Repositories {
		w := r.Worktree
		if filepath.Dir(w.Path) != p.Path || w.Path == p.Path || paths[w.Path] || tokens[w.Token] || w.Token == "" || w.Branch != "feature/shared" || w.Repo != sources[i].Repo {
			t.Fatal("workspace allocation boundaries or ownership were lost")
		}
		paths[w.Path], tokens[w.Token] = true, true
	}
	if p.Repositories[1].Worktree.Base != "develop" || p.Repositories[2].Worktree.Base != "HEAD" {
		t.Fatal("per-repository bases were lost")
	}
	raw, _ := json.Marshal(p)
	var public Interactive
	json.Unmarshal(raw, &public)
	public.RedactOwnership()
	encoded, _ := json.Marshal(public)
	for token := range tokens {
		if strings.Contains(string(encoded), token) {
			t.Fatal("nested ownership token exposed")
		}
	}
	if p.Token == "" || p.Repositories[0].Worktree.Token == "" {
		t.Fatal("redaction changed durable ownership")
	}
	for _, invalid := range [][]RepositorySource{nil, {{Repo: "relative"}}, {{Repo: "/same"}, {Repo: "/same/../same"}}} {
		if _, err := PlanMultiWorkspace(invalid, 1, InteractiveOptions{}); err == nil {
			t.Fatal("invalid repository set accepted")
		}
	}
}
