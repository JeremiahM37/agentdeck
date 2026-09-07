package state

import "testing"

func TestLegalTransitions(t *testing.T) {
	for _, tc := range [][2]string{
		{"backlog", "queued"}, {"queued", "running"}, {"running", "review"},
		{"review", "done"}, {"review", "queued"}, {"running", "failed"},
		{"failed", "queued"}, {"queued", "cancelled"}, {"cancelled", "backlog"},
	} {
		if err := Check(tc[0], tc[1]); err != nil {
			t.Errorf("%s → %s should be legal: %v", tc[0], tc[1], err)
		}
	}
}

func TestIllegalTransitions(t *testing.T) {
	for _, tc := range [][2]string{
		{"backlog", "running"}, {"backlog", "review"}, {"done", "queued"},
		{"done", "backlog"}, {"running", "done"}, {"queued", "review"},
		{"review", "running"}, {"cancelled", "running"},
	} {
		if err := Check(tc[0], tc[1]); err == nil {
			t.Errorf("%s → %s should be rejected", tc[0], tc[1])
		}
	}
}

func TestSameStatusIsNoop(t *testing.T) {
	if err := Check("running", "running"); err != nil {
		t.Fatalf("staying put must be allowed: %v", err)
	}
}

func TestUnknownStatusRejected(t *testing.T) {
	if err := Check("backlog", "warp"); err == nil {
		t.Fatal("an unknown status must be rejected")
	}
}
