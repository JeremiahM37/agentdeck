package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/sessions"
)

func recoveryCheckpoint(cfg *config.Config, args []string) error {
	if len(args) != 2 || (args[0] != "export" && args[0] != "import") {
		return errors.New("usage: agentdeck recovery-checkpoint export|import PATH")
	}
	path, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	if args[0] == "export" {
		m, err := sessions.ExportCheckpoint(context.Background(), cfg.DBPath, func(name string) bool { return exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil })
		if err != nil {
			return err
		}
		if err = sessions.WriteCheckpoint(path, m); err != nil {
			return err
		}
		fmt.Printf("exported %d session checkpoints to %s\n", len(m.Sessions), path)
		return nil
	}
	n, err := sessions.ImportCheckpoint(path, cfg.DBPath, "")
	if err != nil {
		return err
	}
	fmt.Printf("imported %d session checkpoints\n", n)
	return nil
}
