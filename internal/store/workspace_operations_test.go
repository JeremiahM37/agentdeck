package store

import (
	"path/filepath"
	"testing"
)

func TestWorkspaceOperationReservationAndLateCompletion(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	target, err := db.InsertTarget(&Target{Name: "local", Kind: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.InsertSession(&Session{Name: "group", TargetID: target.ID, WorktreeJSON: "original"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sessions SET worktree_json=? WHERE id=?`, "original", session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginWorkspaceOperation(session.ID, "stale", "wrong"); err == nil {
		t.Fatal("stale reservation accepted")
	}
	rows, err := db.WorkspaceOperations(session.ID, false)
	if err != nil || len(rows) != 0 {
		t.Fatal("failed reservation left operation row")
	}
	first, err := db.BeginWorkspaceOperation(session.ID, "original", "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginWorkspaceOperation(session.ID, "first", "overlap"); err == nil {
		t.Fatal("overlapping operation accepted")
	}
	if err := db.CancelWorkspaceOperation(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishWorkspaceOperation(first.ID, "failed", "interrupted", "retained"); err != nil {
		t.Fatal(err)
	}
	stored, _ := db.WorkspaceOperation(first.ID)
	if stored.State != "cancelled" {
		t.Fatal("completion lost concurrently recorded cancellation")
	}
	second, err := db.BeginWorkspaceOperation(session.ID, "retained", "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishWorkspaceOperation(first.ID, "complete", "", "stale-first-result"); err != nil {
		t.Fatal(err)
	}
	current, _ := db.Session(session.ID)
	if current.WorktreeJSON != "second" {
		t.Fatal("late old worker overwrote newer allocation")
	}
	active, err := db.WorkspaceOperations(session.ID, true)
	if err != nil || len(active) != 1 || active[0].ID != second.ID {
		t.Fatal("wrong active operation")
	}
}
