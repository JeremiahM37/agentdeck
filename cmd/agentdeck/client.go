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
