package worktree

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/executor"
)

// Interactive records an allocation before any remote Git mutation. A failed
// launch keeps this record so the directory is still discoverable and removable.
type Interactive struct {
	Repo         string                `json:"repo"`
	Path         string                `json:"path"`
	Branch       string                `json:"branch"`
	Base         string                `json:"base"`
	Commit       string                `json:"commit"`
	Token        string                `json:"token,omitempty"`
	State        string                `json:"state"`
	Error        string                `json:"error,omitempty"`
	Repositories []WorkspaceRepository `json:"repositories,omitempty"`
}
type InteractiveOptions struct {
	Base              string                `json:"base"`
	Branch            string                `json:"branch"`
	ExtraRepositories []RepositorySelection `json:"extra_repositories,omitempty"`
}

type RepositorySelection struct {
	ProjectID int64  `json:"project_id"`
	Base      string `json:"base,omitempty"`
}

func PlanInteractive(repo string, id int64, o InteractiveOptions) *Interactive {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	key := hex.EncodeToString(token)
	branch := strings.TrimSpace(o.Branch)
	if branch == "" {
		branch = fmt.Sprintf("adk/session%d-%s", id, key[:8])
	}
	base := strings.TrimSpace(o.Base)
	if base == "" {
		base = "HEAD"
	}
	return &Interactive{Repo: repo, Path: fmt.Sprintf("%s/session%d-%s", DefaultWorkroot(repo), id, key[:8]), Branch: branch, Base: base, Token: key, State: "creating"}
}

//go:embed interactive.py
var interactiveScript string

func RunInteractive(ctx context.Context, ex executor.Executor, action string, plan *Interactive) error {
	script := interactiveScript
	extra := ""
	if len(plan.Repositories) > 0 {
		if action == "check-create" {
			script = multiPreflightScript
		} else {
			script = multiWorkerScript
			extra = " " + executor.ShellQuote(multiPreflightScript) + " " + executor.ShellQuote(interactiveScript)
		}
	}
	data, _ := json.Marshal(plan)
	result, err := ex.Run(ctx, "python3 -c "+executor.ShellQuote(script)+" "+executor.ShellQuote(action)+" "+executor.ShellQuote(string(data))+extra, executor.RunOpts{Timeout: 120})
	if err != nil {
		return err
	}
	var out struct {
		Workspace *Interactive `json:"workspace"`
		Error     string       `json:"error"`
	}
	if json.Unmarshal([]byte(result.Stdout), &out) != nil {
		return fmt.Errorf("worktree operation failed on target: %s", strings.TrimSpace(result.Stderr))
	}
	if !result.OK() {
		if out.Workspace != nil {
			*plan = *out.Workspace
		}
		return fmt.Errorf("%s", out.Error)
	}
	if out.Workspace == nil {
		return fmt.Errorf("target returned no worktree record")
	}
	*plan = *out.Workspace
	return nil
}
