package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"golang.org/x/term"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/console"
)

const clientHelp = `AgentDeck — web and terminal control

  agentdeck                         Open the dashboard in an interactive terminal
  agentdeck serve                   Start the control-plane server
  agentdeck console                 Live terminal dashboard (also: tui)
  agentdeck console --plain         Line-oriented menu for pipes / accessibility
  agentdeck attach KIND ID          Join tmux (Ctrl-b d returns to console)
  agentdeck api METHOD /path [JSON|@file|-]
  agentdeck upload KIND ID FILE     Add a local file as agent context
  agentdeck files KIND ID [PATH]    Browse files on the agent's machine
  agentdeck download KIND ID REMOTE LOCAL
  agentdeck agent list
  agentdeck agent save JSON|@file|-
  agentdeck skill list PROJECT [--agent claude|codex]
  agentdeck skill attached PROJECT [--agent claude|codex]
  agentdeck skill attach PROJECT SKILL_ID [--agent claude|codex]
  agentdeck skill detach PROJECT ATTACHMENT_ID
  agentdeck mcp                     MCP on standard input/output
  agentdeck version

KIND: session, attempt, project (upload also accepts task).
API paths can omit /api. JSON goes to stdout; errors go to stderr.
Examples:
  agentdeck api GET /sessions
  agentdeck api POST /tasks/12/takeover '{}'
  agentdeck api PATCH /routines/3 '{"enabled":false}'
  agentdeck api POST /sessions/4/send '{"text":"Run the tests"}'
  agentdeck upload session 4 ./requirements.pdf
  agentdeck agent list
  agentdeck agent save @agents.json

AGENTDECK_API sets the server URL (default http://127.0.0.1:9110).
AGENTDECK_AUTH_TOKEN supplies bearer authentication.
AGENTDECK_ATTACH_HOST sets an SSH alias for native attachment to a remote server.
All web operations use this same API. See docs/terminal-client.md for the catalog.
`

func clientCommand(cfg *config.Config, command string, args []string) error {
	if command == "help" || command == "--help" || command == "-h" || len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Print(clientHelp)
		return nil
	}
	c := console.New(env("AGENTDECK_API", "http://127.0.0.1:"+strconv.Itoa(cfg.Port)), cfg.AuthToken)
	var data []byte
	var err error
	switch command {
	case "console", "tui":
		attachClient := func(kind, id string) error {
			argv, e := attachmentCommand(cfg, []string{kind, id})
			if e != nil {
				return e
			}
			argv = attachmentInWorkspace(argv, os.Getenv("TMUX"))
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
		if len(args) > 0 && (len(args) != 1 || args[0] != "--plain") {
			return fmt.Errorf("usage: agentdeck console [--plain]")
		}
		if len(args) == 0 && interactiveTerminal() {
			return console.RunDashboard(c, os.Stdin, os.Stdout, attachClient)
		}
		return console.NewUI(c, os.Stdin, os.Stdout, attachClient).Run()
	case "api":
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("usage: agentdeck api METHOD /path [JSON|@file|-]")
		}
		var body io.Reader
		if len(args) == 3 {
			b := []byte(args[2])
			if args[2] == "-" {
				b, err = io.ReadAll(os.Stdin)
			} else if strings.HasPrefix(args[2], "@") {
				b, err = os.ReadFile(args[2][1:])
			}
			if err != nil {
				return err
			}
			if !json.Valid(b) {
				return fmt.Errorf("request body is not valid JSON")
			}
			body = bytes.NewReader(b)
		}
		data, err = c.Request(strings.ToUpper(args[0]), args[1], body, "application/json")
	case "skill":
		data, err = skillCommand(c, args)
	case "agent":
		data, err = agentCommand(c, args)
	case "upload":
		if len(args) != 3 {
			return fmt.Errorf("usage: agentdeck upload KIND ID FILE")
		}
		if err = validateTerminal(args[:2], true); err != nil {
			return err
		}
		data, err = c.Upload(args[0], args[1], args[2])
	case "files", "download":
		if command == "files" && (len(args) < 2 || len(args) > 3) || command == "download" && len(args) != 4 {
			return fmt.Errorf("usage: agentdeck files KIND ID [PATH] | download KIND ID REMOTE LOCAL")
		}
		if err = validateTerminal(args[:2], false); err != nil {
			return err
		}
		suffix := "/files"
		if command == "download" {
			suffix = "/file"
		}
		path := "/term/" + args[0] + "/" + args[1] + suffix
		if len(args) > 2 {
			path += "?path=" + url.QueryEscape(args[2])
		}
		if command == "download" {
			return c.Download(path, args[3])
		}
		data, err = c.JSON("GET", path, nil)
	default:
		return fmt.Errorf("unknown client command")
	}
	if err != nil {
		return err
	}
	if len(data) > 0 {
		_, err = os.Stdout.Write(data)
		if err == nil && !bytes.HasSuffix(data, []byte("\n")) {
			fmt.Println()
		}
	}
	return err
}

// agentCommand makes the runner registry discoverable without requiring users
// to hand craft an API request. A save replaces the custom definitions exactly
// as the Settings → Agents editor does; JSON can be read from a file or stdin.
func agentCommand(c *console.Client, args []string) ([]byte, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("usage: agentdeck agent list | save JSON|@file|-")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return nil, fmt.Errorf("usage: agentdeck agent list")
		}
		return c.Request("GET", "/agents", nil, "application/json")
	case "save":
		if len(args) != 2 {
			return nil, fmt.Errorf("usage: agentdeck agent save JSON|@file|-")
		}
		b := []byte(args[1])
		var err error
		if args[1] == "-" {
			b, err = io.ReadAll(os.Stdin)
		} else if strings.HasPrefix(args[1], "@") {
			b, err = os.ReadFile(args[1][1:])
		}
		if err != nil {
			return nil, err
		}
		if !json.Valid(b) {
			return nil, fmt.Errorf("agent definitions are not valid JSON")
		}
		return c.Request("PUT", "/agents", bytes.NewReader(b), "application/json")
	default:
		return nil, fmt.Errorf("unknown agent operation %q (use list or save)", args[0])
	}
}

func skillCommand(c *console.Client, args []string) ([]byte, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: agentdeck skill list|attached|attach|detach PROJECT ...")
	}
	op, project := args[0], args[1]
	agent := ""
	for i := 2; i < len(args); i++ {
		if args[i] == "--agent" && i+1 < len(args) {
			agent = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "--") {
			return nil, fmt.Errorf("unknown skill option %s", args[i])
		}
	}
	var method, path string
	var body io.Reader
	switch op {
	case "list":
		method = "GET"
		path = "/api/skills?project_id=" + url.QueryEscape(project)
		if agent != "" {
			path += "&agent=" + url.QueryEscape(agent)
		}
	case "attached":
		method = "GET"
		path = "/api/projects/" + url.PathEscape(project) + "/skills"
		if agent != "" {
			path += "?agent=" + url.QueryEscape(agent)
		}
	case "attach":
		if len(args) < 3 {
			return nil, fmt.Errorf("usage: agentdeck skill attach PROJECT SKILL_ID [--agent claude|codex]")
		}
		method = "POST"
		path = "/api/projects/" + url.PathEscape(project) + "/skills"
		b, _ := json.Marshal(map[string]any{"skill_id": args[2], "agent": agent})
		body = bytes.NewReader(b)
	case "detach":
		if len(args) < 3 {
			return nil, fmt.Errorf("usage: agentdeck skill detach PROJECT ATTACHMENT_ID")
		}
		method = "DELETE"
		path = "/api/projects/" + url.PathEscape(project) + "/skills/" + url.PathEscape(args[2])
	default:
		return nil, fmt.Errorf("unknown skill operation %q", op)
	}
	return c.Request(method, path, body, "application/json")
}
func validateTerminal(args []string, task bool) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: agentdeck attach KIND ID")
	}
	switch args[0] {
	case "session", "attempt", "project", "session-shell", "attempt-shell":
	case "task":
		if !task {
			return fmt.Errorf("use the task's attempt ID for terminal access")
		}
	default:
		return fmt.Errorf("invalid terminal kind")
	}
	id, e := strconv.ParseInt(args[1], 10, 64)
	if e != nil || id <= 0 {
		return fmt.Errorf("invalid terminal ID")
	}
	return nil
}

func interactiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
