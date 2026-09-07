package mcp

import (
	"fmt"
	"net/url"
	"strings"
)

// tool is one MCP tool: its schema, and what it does.
type tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Run         func(s *Server, args map[string]any) (any, error)
}

func obj(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }
func flag(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

func argStr(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func argInt(args map[string]any, key string) int64 {
	switch v := args[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	}
	return 0
}

func argBool(args map[string]any, key string, def bool) bool {
	if b, ok := args[key].(bool); ok {
		return b
	}
	return def
}

// tools is the whole surface an MCP client sees.
var tools = []tool{
	{
		Name:        "board_summary",
		Description: "Current board state: task counts per column and the pending approval count.",
		Schema:      obj(map[string]any{}),
		Run: func(s *Server, _ map[string]any) (any, error) {
			return s.object("/health")
		},
	},
	{
		Name:        "list_projects",
		Description: "Projects that tasks can be filed against (name, target, repo).",
		Schema:      obj(map[string]any{}),
		Run: func(s *Server, _ map[string]any) (any, error) {
			rows, err := s.list("/projects")
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0, len(rows))
			for _, p := range rows {
				out = append(out, map[string]any{"id": p["id"], "name": p["name"],
					"target": p["target_name"], "repo": p["repo_path"]})
			}
			return out, nil
		},
	},
	{
		Name: "list_tasks",
		Description: "List tasks, optionally filtered by status " +
			"(backlog|queued|running|review|done|failed|cancelled).",
		Schema: obj(map[string]any{"status": str("status to filter by")}),
		Run: func(s *Server, args map[string]any) (any, error) {
			path := "/tasks"
			if status := argStr(args, "status"); status != "" {
				path += "?status=" + url.QueryEscape(status)
			}
			rows, err := s.list(path)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, 0, len(rows))
			for _, t := range rows {
				out = append(out, map[string]any{"id": t["id"], "title": t["title"],
					"status": t["status"], "project": t["project_name"],
					"target": t["target_name"], "created_by": t["created_by"]})
			}
			return out, nil
		},
	},
	{
		Name: "create_task",
		Description: "File a coding task. `project` is a project NAME (see list_projects). " +
			"dispatch=true starts an agent immediately; false parks it in the backlog.",
		Schema: obj(map[string]any{
			"project":         str("project name"),
			"title":           str("short card title"),
			"prompt":          str("what the agent should do"),
			"dispatch":        flag("start an agent now (default true)"),
			"model":           str("model override, e.g. sonnet"),
			"permission_mode": str("default|acceptEdits|plan|bypassPermissions"),
		}, "project", "title", "prompt"),
		Run: func(s *Server, args map[string]any) (any, error) {
			name := argStr(args, "project")
			projects, err := s.list("/projects")
			if err != nil {
				return nil, err
			}
			var match map[string]any
			var names []string
			for _, p := range projects {
				pn, _ := p["name"].(string)
				names = append(names, pn)
				if pn == name {
					match = p
				}
			}
			if match == nil {
				return nil, fmt.Errorf("no project named %q — have: %s",
					name, strings.Join(names, ", "))
			}
			mode := argStr(args, "permission_mode")
			if mode == "" {
				mode = "acceptEdits"
			}
			created, err := s.api("POST", "/tasks", map[string]any{
				"project_id": match["id"], "title": argStr(args, "title"),
				"prompt": argStr(args, "prompt"), "model": argStr(args, "model"),
				"permission_mode": mode})
			if err != nil {
				return nil, err
			}
			task, _ := created.(map[string]any)
			id := int64(task["id"].(float64))
			if argBool(args, "dispatch", true) {
				if _, err := s.api("POST", fmt.Sprintf("/tasks/%d/dispatch", id),
					map[string]any{}); err != nil {
					return nil, err
				}
				if fresh, err := s.object(fmt.Sprintf("/tasks/%d", id)); err == nil {
					task = fresh
				}
			}
			return map[string]any{"task_id": id, "status": task["status"],
				"url": fmt.Sprintf("%s/#task/%d", s.API, id)}, nil
		},
	},
	{
		Name:        "task_status",
		Description: "Status, attempt result, verify outcome and diff stats for a task.",
		Schema: obj(map[string]any{
			"task_id":        num("task id"),
			"include_events": flag("append the last 15 timeline events"),
		}, "task_id"),
		Run: func(s *Server, args map[string]any) (any, error) {
			id := argInt(args, "task_id")
			t, err := s.object(fmt.Sprintf("/tasks/%d", id))
			if err != nil {
				return nil, err
			}
			out := map[string]any{"id": t["id"], "title": t["title"],
				"status": t["status"], "attempt": t["attempt"]}
			if argBool(args, "include_events", false) {
				evs, err := s.list(fmt.Sprintf("/tasks/%d/events", id))
				if err != nil {
					return nil, err
				}
				if len(evs) > 15 {
					evs = evs[len(evs)-15:]
				}
				tail := make([]map[string]any, 0, len(evs))
				for _, e := range evs {
					tail = append(tail, map[string]any{"type": e["type"], "payload": e["payload"]})
				}
				out["events_tail"] = tail
			}
			return out, nil
		},
	},
	{
		Name:        "task_diff",
		Description: "The unified diff a finished task produced, per file.",
		Schema:      obj(map[string]any{"task_id": num("task id")}, "task_id"),
		Run: func(s *Server, args map[string]any) (any, error) {
			return s.object(fmt.Sprintf("/tasks/%d/diff", argInt(args, "task_id")))
		},
	},
	{
		Name:        "pending_approvals",
		Description: "Approvals waiting on a human — tool name, input, and owning task.",
		Schema:      obj(map[string]any{}),
		Run: func(s *Server, _ map[string]any) (any, error) {
			return s.list("/approvals?status=pending")
		},
	},
	{
		Name:        "decide_approval",
		Description: "Approve or deny a pending approval. decision: approved|denied.",
		Schema: obj(map[string]any{
			"approval_id": num("approval id"),
			"decision":    str("approved or denied"),
			"note":        str("reason, fed back to the agent on a denial"),
		}, "approval_id", "decision"),
		Run: func(s *Server, args map[string]any) (any, error) {
			return s.api("POST",
				fmt.Sprintf("/approvals/%d/decision", argInt(args, "approval_id")),
				map[string]any{"decision": argStr(args, "decision"), "note": argStr(args, "note")})
		},
	},
	{
		Name:        "complete_task",
		Description: "Mark a reviewed task as done.",
		Schema:      obj(map[string]any{"task_id": num("task id")}, "task_id"),
		Run: func(s *Server, args map[string]any) (any, error) {
			return s.api("POST", fmt.Sprintf("/tasks/%d/complete", argInt(args, "task_id")), nil)
		},
	},
	{
		Name:        "request_changes",
		Description: "Send a reviewed task back for another attempt with feedback.",
		Schema: obj(map[string]any{
			"task_id":  num("task id"),
			"feedback": str("what should change"),
		}, "task_id", "feedback"),
		Run: func(s *Server, args map[string]any) (any, error) {
			return s.api("POST", fmt.Sprintf("/tasks/%d/followup", argInt(args, "task_id")),
				map[string]any{"feedback": argStr(args, "feedback")})
		},
	},
}

func toolSchemas() []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
	}
	return out
}

func (s *Server) call(name string, args map[string]any) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	for _, t := range tools {
		if t.Name == name {
			return t.Run(s, args)
		}
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
