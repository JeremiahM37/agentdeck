package worktree

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

//go:embed multi_preflight.py
var multiPreflightScript string

//go:embed multi_worker.py
var multiWorkerScript string

// WorkspaceRepository records each allocation independently, including a failed
// or removed one. The enclosing workspace owns the root directory separately.
type WorkspaceRepository struct {
	Name      string       `json:"name"`
	ProjectID *int64       `json:"project_id,omitempty"`
	Worktree  *Interactive `json:"worktree"`
}

// RepositorySource is resolved from registered projects on one target before
// planning; paths and Git identities must still be verified on that target.
type RepositorySource struct {
	Name, Repo, Base string
	SetupCommand     string
	SetupEnv         map[string]string
	ProjectID        *int64
}

// PlanMultiWorkspace creates only a plan. It never touches Git or the filesystem.
// Each repository has its own ownership token, base and allocation state.
func PlanMultiWorkspace(sources []RepositorySource, id int64, options InteractiveOptions) (*Interactive, error) {
	if len(sources) < 1 || len(sources) > 8 {
		return nil, fmt.Errorf("a workspace needs between one and eight repositories")
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if !filepath.IsAbs(source.Repo) {
			return nil, fmt.Errorf("repository paths must be absolute on the target")
		}
		path := filepath.Clean(source.Repo)
		if seen[path] {
			return nil, fmt.Errorf("a repository was selected more than once")
		}
		seen[path] = true
	}
	root := PlanInteractive(sources[0].Repo, id, options)
	usedNames := map[string]bool{}
	for i, source := range sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			name = filepath.Base(source.Repo)
		}
		slug := repositoryDirectoryName(name)
		baseSlug := slug
		for n := 2; usedNames[slug]; n++ {
			slug = fmt.Sprintf("%s-%d", baseSlug, n)
		}
		usedNames[slug] = true
		base := source.Base
		if i == 0 && base == "" {
			base = options.Base
		}
		child := PlanInteractive(source.Repo, id, InteractiveOptions{Base: base, Branch: root.Branch})
		child.Path = filepath.Join(root.Path, slug)
		child.SetupCommand, child.SetupEnv = source.SetupCommand, source.SetupEnv
		root.Repositories = append(root.Repositories, WorkspaceRepository{Name: name, ProjectID: source.ProjectID, Worktree: child})
	}
	root.Base = root.Repositories[0].Worktree.Base
	return root, nil
}

func repositoryDirectoryName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if b.Len() >= 48 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	if slug := strings.Trim(b.String(), "-"); slug != "" {
		return slug
	}
	return "repository"
}

// RedactOwnership removes all allocation tokens before an API response. It is
// applied to a decoded copy, never to the persisted ownership records.
func (p *Interactive) RedactOwnership() {
	p.Token = ""
	p.ControlToken = ""
	p.SetupEnv = nil
	for _, repo := range p.Repositories {
		if repo.Worktree != nil {
			repo.Worktree.RedactOwnership()
		}
	}
}

// PlanWorkspaceExtension preserves existing ownership and stages one new child.
// The worker must validate the complete plan against the live root receipt.
func PlanWorkspaceExtension(existing *Interactive, source RepositorySource, id int64) (*Interactive, error) {
	if existing == nil || len(existing.Repositories) < 1 || len(existing.Repositories) >= 8 {
		return nil, fmt.Errorf("extend a grouped workspace with fewer than eight repositories")
	}
	if existing.State != "ready" && existing.State != "failed" {
		return nil, fmt.Errorf("workspace is not ready for an extension")
	}
	if !filepath.IsAbs(source.Repo) {
		return nil, fmt.Errorf("repository path must be absolute on target")
	}
	used := map[string]bool{}
	for _, entry := range existing.Repositories {
		if entry.Worktree == nil || entry.Worktree.State != "ready" {
			return nil, fmt.Errorf("recover incomplete repositories before extending the workspace")
		}
		if filepath.Clean(entry.Worktree.Repo) == filepath.Clean(source.Repo) {
			return nil, fmt.Errorf("repository is already in this workspace")
		}
		used[filepath.Base(entry.Worktree.Path)] = true
	}
	data, err := json.Marshal(existing)
	if err != nil {
		return nil, err
	}
	var next Interactive
	if err = json.Unmarshal(data, &next); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(source.Name)
	if name == "" {
		name = filepath.Base(source.Repo)
	}
	slug := repositoryDirectoryName(name)
	stem := slug
	for n := 2; used[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", stem, n)
	}
	child := PlanInteractive(source.Repo, id, InteractiveOptions{Base: source.Base, Branch: existing.Branch})
	child.Path = filepath.Join(existing.Path, slug)
	child.SetupCommand = source.SetupCommand
	child.SetupEnv = source.SetupEnv
	next.Repositories = append(next.Repositories, WorkspaceRepository{Name: name, ProjectID: source.ProjectID, Worktree: child})
	next.ControlToken = PlanInteractive(source.Repo, id, InteractiveOptions{}).Token
	next.State = "extending"
	next.Error = ""
	return &next, nil
}
