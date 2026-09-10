package console

import (
	tea "github.com/charmbracelet/bubbletea"
	"strconv"
	"strings"
)

type workspaceDraft struct {
	selected map[string]string
	order    []string
}

func (m *dashboard) workspaceRepositoryForm(body map[string]any, original *dashboardForm) tea.Cmd {
	var primary row
	for _, p := range m.projects {
		if id(p) == str(body["project_id"]) {
			primary = p
		}
	}
	if primary == nil {
		m.notice = "Primary project is unavailable. Refresh projects and try again."
		return nil
	}
	if original.workspace == nil {
		original.workspace = &workspaceDraft{selected: map[string]string{}}
	}
	selected := original.workspace.selected
	order := []string{}
	for _, projectID := range original.workspace.order {
		valid := false
		for _, p := range m.projects {
			if id(p) == projectID && id(p) != id(primary) && str(p["target_id"]) == str(primary["target_id"]) {
				valid = true
			}
		}
		if valid {
			order = append(order, projectID)
		} else {
			delete(selected, projectID)
		}
	}
	original.workspace.order = order
	var show func() tea.Cmd
	show = func() tea.Cmd {
		original.workspace.order = order
		actions := []choice{{"Create session", "create"}, {"Back to session settings", "back"}}
		for _, p := range m.projects {
			if id(p) == id(primary) || str(p["target_id"]) != str(primary["target_id"]) {
				continue
			}
			if _, ok := selected[id(p)]; ok {
				actions = append(actions, choice{"Edit or remove " + name(p), "edit/" + id(p)})
			} else if len(selected) < 7 {
				actions = append(actions, choice{"Add " + name(p), "add/" + id(p)})
			}
		}
		value := "create"
		if len(selected) == 0 && len(actions) > 2 {
			value = actions[2].Value
		}
		action := optionField("action", "Repository action", value, actions, true)
		action.Compact = true
		summary := []string{name(primary) + " (primary)"}
		for _, id := range order {
			if base, ok := selected[id]; ok {
				for _, p := range m.projects {
					if str(p["id"]) == id {
						if base == "" {
							base = "HEAD"
						}
						summary = append(summary, name(p)+" @ "+base)
					}
				}
			}
		}
		cmd := m.openForm("Workspace repositories: "+strings.Join(summary, ", "), []field{action}, func(values map[string]any) tea.Cmd {
			action := str(values["action"])
			if action == "back" {
				m.form = original
				return m.focusField()
			}
			if action == "create" {
				extra := []map[string]any{}
				for _, id := range order {
					if base, ok := selected[id]; ok {
						projectID, _ := strconv.ParseInt(id, 10, 64)
						extra = append(extra, map[string]any{"project_id": projectID, "base": base})
					}
				}
				body["worktree"].(map[string]any)["extra_repositories"] = extra
				return m.request("Create session", "POST", "/sessions", body, false)
			}
			parts := strings.SplitN(action, "/", 2)
			if len(parts) != 2 {
				return nil
			}
			id := parts[1]
			var project row
			for _, p := range m.projects {
				if str(p["id"]) == id {
					project = p
				}
			}
			if project == nil {
				return nil
			}
			fields := []field{{Key: "base", Label: "Base (blank uses committed HEAD)", Value: selected[id]}}
			if parts[0] == "edit" {
				fields = append(fields, optionField("operation", "Action", "save", []choice{{"Save base", "save"}, {"Remove repository", "remove"}}, true))
			}
			cmd := m.openForm("Repository: "+name(project), fields, func(values map[string]any) tea.Cmd {
				if values["operation"] == "remove" {
					delete(selected, id)
					for i, v := range order {
						if v == id {
							order = append(order[:i], order[i+1:]...)
							break
						}
					}
				} else {
					if _, ok := selected[id]; !ok {
						order = append(order, id)
					}
					selected[id] = strings.TrimSpace(str(values["base"]))
				}
				return show()
			})
			m.form.cancel = show
			return cmd
		})
		m.form.cancel = func() tea.Cmd { m.form = original; return m.focusField() }
		return cmd
	}
	if len(m.projects) == 0 {
		return nil
	}
	return show()
}
