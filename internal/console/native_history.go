package console

import (
	"encoding/json"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"net/url"
	"strings"
	"time"
)

type nativeListMsg struct {
	key, session string
	data         []byte
	err          error
}
type nativeSelection struct {
	key, session, conversation string
	before                     *int64
}

func (m *dashboard) savedConversations() tea.Cmd {
	r := m.current()
	if r == nil || sections[m.section] != "sessions" || m.busy {
		return nil
	}
	c, key, sid := m.client, m.key(), id(r)
	m.busy = true
	return func() tea.Msg {
		b, e := c.JSON("GET", "/sessions/"+sid+"/conversations", nil)
		return nativeListMsg{key, sid, b, e}
	}
}
func (m *dashboard) nativePicker(v nativeListMsg) tea.Cmd {
	m.busy = false
	if v.key != m.key() {
		return nil
	}
	if v.err != nil {
		m.notice = v.err.Error()
		return nil
	}
	var data struct {
		Conversations []struct {
			ID, Title string
			Modified  float64
		} `json:"conversations"`
		Limited       bool `json:"scan_limited"`
		ForkSupported bool `json:"fork_supported"`
	}
	if err := json.Unmarshal(v.data, &data); err != nil {
		m.notice = err.Error()
		return nil
	}
	if len(data.Conversations) == 0 {
		m.notice = "No saved conversations found in this workspace."
		return nil
	}
	choices := []choice{}
	for _, c := range data.Conversations {
		choices = append(choices, choice{time.Unix(int64(c.Modified), 0).Format("Jan 2 15:04") + " · " + clean(c.Title) + " · " + c.ID, c.ID})
	}
	actions := []choice{{"Read saved messages", "read"}}
	if data.ForkSupported {
		actions = append(actions, choice{"Fork into a new conversation", "fork"})
	}
	fields := []field{{Key: "conversation", Label: "Saved conversation (this workspace)", Value: choices[0].Value, Options: choices}, {Key: "action", Label: "Action", Value: "read", Options: actions}, {Key: "name", Label: "Fork name", Value: str(m.current()["name"]) + " · fork"}}
	return m.openForm("Saved conversations", fields, func(values map[string]any) tea.Cmd {
		cid := str(values["conversation"])
		m.form = nil
		if str(values["action"]) == "fork" {
			m.pending = &dashboardAction{Label: "Fork conversation", Method: "POST", Path: "/sessions/" + v.session + "/fork", Body: map[string]any{"conversation_id": cid, "name": values["name"]}, Warning: "Create a new conversation from " + cid + "? Both agents share the workspace files. The original keeps running."}
			return nil
		}
		m.native = &nativeSelection{key: v.key, session: v.session, conversation: cid}
		return m.readResource("Saved conversation", "/sessions/"+v.session+"/conversations/"+url.PathEscape(cid))
	})
}
func (m *dashboard) olderNative() tea.Cmd {
	n := m.native
	if n == nil || n.key != m.key() || n.before == nil || m.busy {
		return nil
	}
	return m.readResource("Saved conversation", fmt.Sprintf("/sessions/%s/conversations/%s?before=%d", n.session, url.PathEscape(n.conversation), *n.before))
}
func nativeText(data []byte) string {
	var page struct {
		Messages []struct {
			Role, Text string
			Truncated  bool
		}
		Before *int64
	}
	if json.Unmarshal(data, &page) != nil {
		return "Could not parse saved messages"
	}
	var out []string
	for _, message := range page.Messages {
		text := strings.ToUpper(message.Role) + "\n" + message.Text
		if message.Truncated {
			text += "\n[Long message shortened]"
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		out = append(out, "No readable messages in this window.")
	}
	if page.Before != nil {
		out = append(out, "Press O to load earlier messages; H to choose another conversation.")
	}
	return strings.Join(out, "\n\n")
}
