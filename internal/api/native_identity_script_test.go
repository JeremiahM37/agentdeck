package api_test

import (
	"os/exec"
	"testing"
)

func TestNativeIdentityScriptBoundaries(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	if output, err := exec.Command(python, "scripts/native_identity_test.py").CombinedOutput(); err != nil {
		t.Fatalf("native identity boundary tests: %v\n%s", err, output)
	}
}

func TestNativeSearchIndexScript(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	if output, err := exec.Command(python, "-W", "error::ResourceWarning", "scripts/native_search_test.py").CombinedOutput(); err != nil {
		t.Fatalf("native search index tests: %v\n%s", err, output)
	}
}
