package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/JeremiahM37/agentdeck/internal/store"
)

var checkpointTmuxName = regexp.MustCompile(`^adk-s[0-9]+$`)

type CheckpointManifest struct {
	Version  int                 `json:"version"`
	Created  string              `json:"created_at"`
	BootID   string              `json:"source_boot_id"`
	Sessions []CheckpointSession `json:"sessions"`
}

type CheckpointSession struct {
	ID                int64  `json:"id"`
	TargetID          int64  `json:"target_id"`
	TmuxSession       string `json:"tmux_session"`
	Workdir           string `json:"workdir"`
	BootID            string `json:"boot_id"`
	NativeRecoveryCID string `json:"native_recovery_cid"`
}

func ReadBootID() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if !linuxBootID.MatchString(id) {
		return "", errors.New("invalid local boot id")
	}
	return id, nil
}

// ExportCheckpoint reads without running migrations or taking a write lock.
// It refuses rows lacking the recovery columns or native identity: a partial
// manifest cannot safely drive exact resume after a restart.
func ExportCheckpoint(ctx context.Context, dbPath string, validateTmux func(string) bool) (CheckpointManifest, error) {
	boot, err := ReadBootID()
	if err != nil {
		return CheckpointManifest{}, err
	}
	db, err := sql.Open("sqlite", dbPath+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return CheckpointManifest{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	cols := map[string]bool{}
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(sessions)")
	if err != nil {
		return CheckpointManifest{}, err
	}
	for rows.Next() {
		var n, typ string
		var cid, notnull, pk int
		var d any
		if err := rows.Scan(&cid, &n, &typ, &notnull, &d, &pk); err != nil {
			rows.Close()
			return CheckpointManifest{}, err
		}
		cols[n] = true
	}
	rows.Close()
	if !cols["boot_id"] || !cols["native_recovery_cid"] {
		return CheckpointManifest{}, errors.New("database has no recovery columns; start the recovery-capable engine once before exporting")
	}
	q, err := db.QueryContext(ctx, `SELECT id,target_id,tmux_session,workdir,boot_id,native_recovery_cid FROM sessions WHERE ended_at IS NULL AND archived_at IS NULL AND origin='agentdeck' ORDER BY id`)
	if err != nil {
		return CheckpointManifest{}, err
	}
	defer q.Close()
	m := CheckpointManifest{Version: 1, Created: time.Now().UTC().Format(time.RFC3339Nano), BootID: boot}
	for q.Next() {
		var s CheckpointSession
		if err := q.Scan(&s.ID, &s.TargetID, &s.TmuxSession, &s.Workdir, &s.BootID, &s.NativeRecoveryCID); err != nil {
			return CheckpointManifest{}, err
		}
		if !checkpointTmuxName.MatchString(s.TmuxSession) {
			return CheckpointManifest{}, fmt.Errorf("session %d has invalid tmux name", s.ID)
		}
		if s.BootID == "" || s.NativeRecoveryCID == "" {
			return CheckpointManifest{}, fmt.Errorf("session %d lacks boot/native identity", s.ID)
		}
		if validateTmux != nil && !validateTmux(s.TmuxSession) {
			return CheckpointManifest{}, fmt.Errorf("tmux session %q is not live", s.TmuxSession)
		}
		m.Sessions = append(m.Sessions, s)
	}
	return m, q.Err()
}

func WriteCheckpoint(path string, m CheckpointManifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + fmt.Sprintf(".partial-%d", os.Getpid())
	if err = os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
func ReadCheckpoint(path string) (CheckpointManifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return CheckpointManifest{}, err
	}
	var m CheckpointManifest
	if err = json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Version != 1 || m.BootID == "" {
		return m, errors.New("invalid checkpoint manifest")
	}
	return m, nil
}

// ImportCheckpoint migrates through the normal store path, then updates only
// rows that still match the exported identity. It is idempotent and refuses
// target/tmux/workdir drift.
func ImportCheckpoint(path, dbPath, currentBoot string) (int, error) {
	m, err := ReadCheckpoint(path)
	if err != nil {
		return 0, err
	}
	if currentBoot == "" {
		currentBoot, err = ReadBootID()
		if err != nil {
			return 0, err
		}
	}
	db, err := store.Open(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, s := range m.Sessions {
		var target, tmux, workdir, boot, native string
		err := tx.QueryRow(`SELECT target_id,tmux_session,workdir,boot_id,native_recovery_cid FROM sessions WHERE id=? AND ended_at IS NULL`, s.ID).Scan(&target, &tmux, &workdir, &boot, &native)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("session %d is missing or ended", s.ID)
		}
		if err != nil {
			return 0, err
		}
		if target != fmt.Sprint(s.TargetID) || tmux != s.TmuxSession || workdir != s.Workdir {
			return 0, fmt.Errorf("session %d identity changed", s.ID)
		}
		if native != "" && native != s.NativeRecoveryCID {
			return 0, fmt.Errorf("session %d native identity changed", s.ID)
		}
		if _, err = tx.Exec(`UPDATE sessions SET boot_id=?,native_recovery_cid=? WHERE id=? AND ended_at IS NULL`, currentBoot, s.NativeRecoveryCID, s.ID); err != nil {
			return 0, err
		}
		n++
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
