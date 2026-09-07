package executor

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"strings"
)

// Pct drives an LXC on this Proxmox node through `pct exec` — no SSH and no
// per-container keys. The control-plane user needs passwordless sudo for pct.
// Target kind 'pct', with host holding the vmid.
type Pct struct {
	VMID  string
	local *Local

	// runner is the command path, injectable so tests can assert on the exact
	// shell command an operation builds without touching a real container.
	runner func(context.Context, string, RunOpts) (Result, error)
}

// NewPct builds a pct executor for a container id.
func NewPct(vmid string) *Pct { return &Pct{VMID: vmid, local: NewLocal()} }

// Wrap renders the `sudo pct exec` invocation for a command. Exported because
// the quoting round-trip is worth testing directly.
func Wrap(vmid, cmd, cwd string) string {
	inner := cmd
	if cwd != "" {
		inner = "cd " + ShellQuote(cwd) + " && " + cmd
	}
	return fmt.Sprintf("sudo pct exec %s -- bash -c %s", ShellQuote(vmid), ShellQuote(inner))
}

// Run executes inside the container.
func (p *Pct) Run(ctx context.Context, cmd string, opts RunOpts) (Result, error) {
	if p.runner != nil {
		return p.runner(ctx, cmd, opts)
	}
	return p.local.Run(ctx, Wrap(p.VMID, cmd, opts.Cwd), RunOpts{Timeout: opts.Timeout})
}

// ReadFile tails a file inside the container from a byte offset.
func (p *Pct) ReadFile(ctx context.Context, filePath string, offset int64) ([]byte, error) {
	r, err := p.Run(ctx, fmt.Sprintf("tail -c +%d %s 2>/dev/null || true",
		offset+1, ShellQuote(filePath)), RunOpts{Timeout: 60})
	if err != nil {
		return nil, err
	}
	return []byte(r.Stdout), nil
}

// WriteFile pushes bytes into the container as base64.
func (p *Pct) WriteFile(ctx context.Context, filePath string, data []byte) error {
	_, err := p.Run(ctx, writeFileCommand(filePath, data), RunOpts{Timeout: 120})
	return err
}

// Close is a no-op: pct exec pools nothing.
func (p *Pct) Close() error { return nil }

// writeFileCommand is the shared "mkdir -p && base64 -d >" recipe used by every
// remote executor, so a staged file lands byte-identically on all target kinds.
func writeFileCommand(filePath string, data []byte) string {
	b64 := base64.StdEncoding.EncodeToString(data)
	parent := path.Dir(filePath)
	var b strings.Builder
	fmt.Fprintf(&b, "mkdir -p %s && echo %s | base64 -d > %s",
		ShellQuote(parent), b64, ShellQuote(filePath))
	return b.String()
}
