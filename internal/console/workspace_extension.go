package console

import (
	"encoding/json"
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *dashboard) extendWorkspaceForm() tea.Cmd {
	r := m.current()
	ws, _ := r["workspace"].(map[string]any)
	repos, _ := ws["repositories"].([]any)
	choices := []choice{}
	for _, project := range m.projects {
		if str(project["target_id"]) != str(r["target_id"]) {
			continue
		}
		exists := false
		for _, entry := range repos {
			repo, _ := entry.(map[string]any)
			wt, _ := repo["worktree"].(map[string]any)
			if str(repo["project_id"]) == id(project) || str(wt["repo"]) == str(project["repo_path"]) {
				exists = true
			}
		}
		if !exists {
			choices = append(choices, choice{name(project), id(project)})
		}
	}
	if len(choices) == 0 {
		m.notice = "No other registered projects on this target"
		return nil
	}
	path := "/sessions/" + id(r) + "/worktree/repositories"
	return m.openForm("Add repository (runs project setup)", []field{optionField("project_id", "Project", choices[0].Value, choices, true), {Key: "base", Label: "Base (blank uses default branch)"}}, func(body map[string]any) tea.Cmd {
		pid, err := strconv.ParseInt(str(body["project_id"]), 10, 64)
		if err != nil {
			m.notice = "Choose a project"
			return nil
		}
		body["project_id"] = pid
		return m.request("Add repository", "POST", path, body, false)
	})
}

func (m *dashboard) workspaceExtensionAction(action string) tea.Cmd {
	path := "/sessions/" + id(m.current()) + "/worktree/operations"
	client := m.client
	return func() tea.Msg {
		data, err := client.JSON("GET", path, nil)
		if err != nil {
			return resultMsg{err: err}
		}
		var rows []struct {
			ID    int64
			State string
		}
		if err = json.Unmarshal(data, &rows); err != nil {
			return resultMsg{err: err}
		}
		for _, op := range rows {
			if op.State != "running" && op.State != "recovering" {
				continue
			}
			data, err = client.JSON("POST", fmt.Sprintf("%s/%d/%s", path, op.ID, action), map[string]any{})
			return resultMsg{label: "Workspace operation check", data: data, err: err}
		}
		return resultMsg{err: fmt.Errorf("no active repository addition")}
	}
}
