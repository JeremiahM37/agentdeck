package api

import (
	"net/http"

	"github.com/JeremiahM37/agentdeck/internal/agents"
	"github.com/JeremiahM37/agentdeck/internal/scheduler"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

var (
	agentNames      = []string{"claude", "codex", "gemini"}
	permissionModes = []string{"default", "acceptEdits", "plan", "bypassPermissions"}
	capProfiles     = []string{"restricted", "parity"}
)

type projectIn struct {
	Name              string  `json:"name"`
	TargetID          int64   `json:"target_id"`
	RepoPath          string  `json:"repo_path"`
	DefaultBaseBranch *string `json:"default_base_branch"`
	WorkrootOverride  string  `json:"workroot_override"`
	VerifyCmd         string  `json:"verify_cmd"`
	KeepWorktrees     bool    `json:"keep_worktrees"`
	ReviewGate        bool    `json:"review_gate"`
	// Env is extra env for the agent — the any-model door (ANTHROPIC_BASE_URL &c).
	Env map[string]any `json:"env"`
	// ContextPaths stack on top of the target's bundle.
	ContextPaths []string       `json:"context_paths"`
	MCP          map[string]any `json:"mcp"`
	StrictMCP    bool           `json:"strict_mcp"`
	Permissions  map[string]any `json:"permissions"`
	GateMatcher  string         `json:"gate_matcher"`
	DefaultAgent *string        `json:"default_agent"`
	// CapabilityProfile 'parity' grants the tools, MCP servers and memory dir a
	// terminal session has; 'restricted' keeps only the rules set explicitly.
	CapabilityProfile *string `json:"capability_profile"`
	// DefaultPermissionMode '' means "use the task default" (acceptEdits). Set
	// 'default' to make every dispatch on this project stop for approval — the
	// right setting when a project's blast radius is infrastructure, not a diff.
	DefaultPermissionMode *string `json:"default_permission_mode"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Projects()
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in projectIn
	if err := decodeBody(r, &in); err != nil {
		httpError(w, 422, "%s", err.Error())
		return
	}
	if in.Name == "" || in.RepoPath == "" {
		httpError(w, 422, "name and repo_path are required")
		return
	}
	if in.DefaultAgent != nil && !oneOf(*in.DefaultAgent, agentNames...) {
		httpError(w, 422, "default_agent must be one of %v", agentNames)
		return
	}
	if in.CapabilityProfile != nil && !oneOf(*in.CapabilityProfile, capProfiles...) {
		httpError(w, 422, "capability_profile must be one of %v", capProfiles)
		return
	}
	if in.DefaultPermissionMode != nil && *in.DefaultPermissionMode != "" &&
		!oneOf(*in.DefaultPermissionMode, permissionModes...) {
		httpError(w, 422, "default_permission_mode must be one of %v", permissionModes)
		return
	}
	if _, err := s.DB.Target(in.TargetID); err != nil {
		httpError(w, 400, "no such target")
		return
	}
	permJSON := store.J(orEmptyMap(in.Permissions))
	// reject typo'd permission keys NOW, not as a mystery denial mid-run
	if _, err := agents.ParsePermissions(permJSON); err != nil {
		httpError(w, 400, "%s", err.Error())
		return
	}
	p := &store.Project{
		Name: in.Name, TargetID: in.TargetID, RepoPath: in.RepoPath,
		DefaultBaseBranch: strOr(in.DefaultBaseBranch, "main"),
		WorkrootOverride:  in.WorkrootOverride, VerifyCmd: in.VerifyCmd,
		KeepWorktrees: boolInt(in.KeepWorktrees), ReviewGate: boolInt(in.ReviewGate),
		EnvJSON:     store.J(orEmptyMap(in.Env)),
		ContextJSON: store.J(orEmpty(in.ContextPaths)),
		MCPJSON:     store.J(orEmptyMap(in.MCP)),
		StrictMCP:   boolInt(in.StrictMCP), PermissionsJSON: permJSON,
		GateMatcher:       in.GateMatcher,
		DefaultAgent:      strOr(in.DefaultAgent, "claude"),
		CapabilityProfile: strOr(in.CapabilityProfile, "restricted"),
	}
	if in.DefaultPermissionMode != nil {
		p.DefaultPermissionMode = *in.DefaultPermissionMode
	}
	out, err := s.DB.InsertProject(p)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 201, out)
}

type projectPatch struct {
	VerifyCmd             *string         `json:"verify_cmd"`
	DefaultBaseBranch     *string         `json:"default_base_branch"`
	KeepWorktrees         *bool           `json:"keep_worktrees"`
	ReviewGate            *bool           `json:"review_gate"`
	Policy                *map[string]any `json:"policy"`
	Env                   *map[string]any `json:"env"`
	ContextPaths          *[]string       `json:"context_paths"`
	MCP                   *map[string]any `json:"mcp"`
	StrictMCP             *bool           `json:"strict_mcp"`
	Permissions           *map[string]any `json:"permissions"`
	GateMatcher           *string         `json:"gate_matcher"`
	DefaultAgent          *string         `json:"default_agent"`
	CapabilityProfile     *string         `json:"capability_profile"`
	DefaultPermissionMode *string         `json:"default_permission_mode"`
}

func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such project")
		return
	}
	var p projectPatch
	if err := decodeBody(r, &p); err != nil {
		httpError(w, 422, "%s", err.Error())
		return
	}
	if p.DefaultAgent != nil && !oneOf(*p.DefaultAgent, agentNames...) {
		httpError(w, 422, "default_agent must be one of %v", agentNames)
		return
	}
	if p.CapabilityProfile != nil && !oneOf(*p.CapabilityProfile, capProfiles...) {
		httpError(w, 422, "capability_profile must be one of %v", capProfiles)
		return
	}
	if p.DefaultPermissionMode != nil && *p.DefaultPermissionMode != "" &&
		!oneOf(*p.DefaultPermissionMode, permissionModes...) {
		httpError(w, 422, "default_permission_mode must be one of %v", permissionModes)
		return
	}
	if _, err := s.DB.Project(id); err != nil {
		httpError(w, 404, "no such project")
		return
	}
	fields := map[string]any{}
	setStr(fields, "verify_cmd", p.VerifyCmd)
	setStr(fields, "default_base_branch", p.DefaultBaseBranch)
	setStr(fields, "gate_matcher", p.GateMatcher)
	setStr(fields, "default_agent", p.DefaultAgent)
	setStr(fields, "capability_profile", p.CapabilityProfile)
	setStr(fields, "default_permission_mode", p.DefaultPermissionMode)
	setBool(fields, "keep_worktrees", p.KeepWorktrees)
	setBool(fields, "review_gate", p.ReviewGate)
	setBool(fields, "strict_mcp", p.StrictMCP)
	setJSON(fields, "policy_json", p.Policy)
	setJSON(fields, "env_json", p.Env)
	setJSON(fields, "mcp_json", p.MCP)
	if p.ContextPaths != nil {
		fields["context_json"] = store.J(orEmpty(*p.ContextPaths))
	}
	if p.Permissions != nil {
		raw := store.J(*p.Permissions)
		if _, err := agents.ParsePermissions(raw); err != nil {
			httpError(w, 400, "%s", err.Error())
			return
		}
		fields["permissions_json"] = raw
	}
	if len(fields) > 0 {
		if err := s.DB.Update("projects", id, fields); err != nil {
			respondErr(w, err)
			return
		}
	}
	out, err := s.DB.Project(id)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// projectCapability reports what this project's agents can ACTUALLY do —
// resolved, not requested.
//
// The UI states facts with this instead of inferring them from config: which MCP
// servers are reachable depends on the target kind, and a memory store is only
// usable if the sandbox was opened for it too.
func (s *Server) projectCapability(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such project")
		return
	}
	proj, err := s.DB.Project(id)
	if err != nil {
		httpError(w, 404, "no such project")
		return
	}
	target, err := s.DB.Target(proj.TargetID)
	if err != nil {
		respondErr(w, err)
		return
	}
	mcp := store.UnjObj(proj.MCPJSON)
	servers := scheduler.EffectiveMCPServers(s.Cfg.HostClaudeConfig, proj, target, mcp)
	perms, err := agents.ParsePermissions(proj.PermissionsJSON)
	if err != nil {
		respondErr(w, err)
		return
	}
	profile := proj.CapabilityProfile
	if profile == "" {
		profile = "restricted"
	}
	settings, err := agents.BuildSettings(agents.SettingsInput{
		Permissions: perms, Profile: profile, MCPServers: servers,
		MemoryDir: target.MemoryDir})
	if err != nil {
		respondErr(w, err)
		return
	}
	resolved, _ := settings["permissions"].(agents.Permissions)

	notes := []string{}
	if profile != "parity" {
		notes = append(notes, "restricted: only rules you set explicitly are granted — "+
			"headless denies everything else without asking")
	}
	if target.Kind != "local" && target.Kind != "mock" && len(mcp) == 0 {
		notes = append(notes, target.Kind+" target has no MCP servers; the host's own "+
			"are not portable (local binaries, secrets in env)")
	}
	if target.MemoryDir == "" {
		notes = append(notes, "no memory store on this target — agents start memory-blind")
	}
	writeJSON(w, 200, map[string]any{
		"profile": profile, "target_kind": target.Kind,
		"mcp_servers": nonNil(servers), "memory_dir": target.MemoryDir,
		"allow": nonNil(resolved.Allow), "deny": nonNil(resolved.Deny),
		"additional_directories": nonNil(resolved.AdditionalDirectories),
		"notes":                  notes,
	})
}

func (s *Server) projectNotes(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such project")
		return
	}
	rows, err := s.DB.ProjectNotes(id, 100)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	id, err1 := pathID(r, "id")
	noteID, err2 := pathID(r, "noteID")
	if err1 != nil || err2 != nil {
		httpError(w, 404, "no such note")
		return
	}
	s.DB.Exec(`DELETE FROM memories WHERE id=? AND project_id=?`, noteID, id)
	w.WriteHeader(204)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such project")
		return
	}
	if s.DB.Exists("tasks", "project_id=?", id) {
		httpError(w, 409, "project has tasks")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM projects WHERE id=?`, id); err != nil {
		respondErr(w, err)
		return
	}
	w.WriteHeader(204)
}

func setStr(fields map[string]any, col string, v *string) {
	if v != nil {
		fields[col] = *v
	}
}

func setBool(fields map[string]any, col string, v *bool) {
	if v != nil {
		fields[col] = boolInt(*v)
	}
}

func setJSON(fields map[string]any, col string, v *map[string]any) {
	if v != nil {
		fields[col] = store.J(*v)
	}
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
