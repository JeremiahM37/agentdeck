package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/config"
)

// Desktop launchers send an ID, never a shell command or a destination host.
// The control plane resolves the current target, port and SSH/WSL wrapper.
func attach(cfg *config.Config, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: agentdeck attach session|attempt|project ID")
	}
	switch args[0] {
	case "session", "attempt", "project", "session-shell", "attempt-shell":
	default:
		return fmt.Errorf("invalid terminal kind")
	}
	id, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("invalid terminal ID")
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/api/term/%s/%d/info", cfg.Port, url.PathEscape(args[0]), id)
	req, _ := http.NewRequest("GET", endpoint, nil)
	if cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("cannot attach: control plane returned %s", res.Status)
	}
	var data struct {
		Argv []string `json:"attach_argv"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return err
	}
	if len(data.Argv) == 0 {
		return fmt.Errorf("no attachment command")
	}
	binary, err := exec.LookPath(data.Argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(binary, data.Argv, os.Environ())
}
