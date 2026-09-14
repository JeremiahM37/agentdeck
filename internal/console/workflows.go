package console

import (
	"encoding/json"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type workflowOption struct {
	ID, Name string
	Enabled  bool
	Commands []string
}

type workflowList struct {
	Workflows []workflowOption `json:"workflows"`
}

type workflowsLoadedMsg struct {
	path, agent string
	providers   map[string]workflowList
	err         error
}

const workflowSessionNotice = "Start a new session to load workflow changes. Existing sessions keep their loaded instructions; generated documents are preserved."

func (m *dashboard) workflowsForm() tea.Cmd {
	r := m.current()
	if r == nil || sections[m.section] != "projects" || m.busy {
		return nil
	}
	path := "/projects/" + id(r) + "/workflows"
	agent := str(r["default_agent"])
	if agent != "claude" && agent != "codex" {
		agent = "claude"
	}
	m.busy = true
	c := m.client
	return func() tea.Msg {
		out := workflowsLoadedMsg{path: path, agent: agent, providers: map[string]workflowList{}}
		for _, provider := range []string{"claude", "codex"} {
			data, err := c.JSON("GET", path+"?agent="+provider, nil)
			if err != nil {
				out.err = err
				return out
			}
			var list workflowList
			if err = json.Unmarshal(data, &list); err != nil {
				out.err = err
				return out
			}
			out.providers[provider] = list
		}
		return out
	}
}

func (m *dashboard) workflowsLoaded(msg workflowsLoadedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = "Workflows: " + clean(msg.err.Error())
		return nil
	}
	choices := []choice{}
	for _, workflow := range msg.providers["claude"].Workflows {
		states := []string{}
		for _, provider := range []string{"claude", "codex"} {
			for _, entry := range msg.providers[provider].Workflows {
				if entry.ID == workflow.ID {
					state := "off"
					if entry.Enabled {
						state = "on"
					}
					states = append(states, provider+" "+state)
				}
			}
		}
		choices = append(choices, choice{clean(workflow.Name + " · " + strings.Join(states, " / ")), workflow.ID})
	}
	if len(choices) == 0 {
		m.notice = "No workflow integrations are available on this server."
		return nil
	}
	m.notice = workflowSessionNotice
	return m.openForm("Project workflows — Spec Kit / Maestro", []field{
		optionField("agent", "Provider", msg.agent, []choice{{"Claude Code", "claude"}, {"Codex", "codex"}}, true),
		optionField("workflow", "Workflow (current state by provider)", choices[0].Value, choices, true),
		optionField("operation", "Set availability", "enable", []choice{{"Enable", "enable"}, {"Disable (preserve documents)", "disable"}}, true),
	}, func(body map[string]any) tea.Cmd {
		agent, workflow, operation := str(body["agent"]), str(body["workflow"]), str(body["operation"])
		if (agent != "claude" && agent != "codex") || (operation != "enable" && operation != "disable") {
			m.notice = "Choose a provider and Enable or Disable."
			return nil
		}
		found := false
		for _, option := range choices {
			found = found || option.Value == workflow
		}
		if !found {
			m.notice = "Choose an available workflow."
			return nil
		}
		return m.request("Workflow saved — start a new session to load changes", "PUT", msg.path+"/"+workflow,
			map[string]any{"agent": agent, "enabled": operation == "enable"}, false)
	})
}

func (u *UI) workflowSettings(path string, project map[string]any) error {
	agent := text(project["default_agent"])
	if agent != "claude" && agent != "codex" {
		agent = "claude"
	}
	for {
		data, err := u.Client.JSON("GET", path+"/workflows?agent="+agent, nil)
		if err != nil {
			return err
		}
		var list workflowList
		if err = json.Unmarshal(data, &list); err != nil {
			return err
		}
		u.say("\nPROJECT WORKFLOWS · %s", agent)
		u.say("%s", workflowSessionNotice)
		for i, workflow := range list.Workflows {
			state := "off"
			if workflow.Enabled {
				state = "on"
			}
			commands := make([]string, 0, len(workflow.Commands))
			prefix := "/"
			if agent == "codex" {
				prefix = "$"
			}
			for _, command := range workflow.Commands {
				commands = append(commands, prefix+command)
			}
			u.say("%d  %s: %s\n   %s", i+1, workflow.Name, state, strings.Join(commands, ", "))
		}
		pick, err := u.ask("Number to toggle · p provider · b back", "")
		if err != nil || pick == "" || pick == "b" {
			return err
		}
		if pick == "p" {
			agent = u.skillProvider(agent)
			continue
		}
		n, err := strconv.Atoi(pick)
		if err != nil || n < 1 || n > len(list.Workflows) {
			u.say("Choose a listed workflow number.")
			continue
		}
		workflow := list.Workflows[n-1]
		_, err = u.Client.JSON("PUT", path+"/workflows/"+workflow.ID, map[string]any{"agent": agent, "enabled": !workflow.Enabled})
		if err != nil {
			u.say("Workflow change failed: %v", err)
		} else {
			u.say("%s availability saved. %s", workflow.Name, workflowSessionNotice)
		}
	}
}
