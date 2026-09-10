package sessions

import "testing"

func TestNormalizeGroup(t *testing.T) {
	for input, want := range map[string]string{"": "", "  ": "", " Work / Client ": "Work/Client", "研究/项目": "研究/项目", "__proto__/constructor": "__proto__/constructor"} {
		got, err := NormalizeGroup(input)
		if err != nil || got != want {
			t.Fatalf("%q => %q %v", input, got, err)
		}
	}
	for _, input := range []string{"/Work", "Work/", "Work//Client", "Work/../Client", "Work/\x1bClient", "a/b/c/d/e/f/g/h/i"} {
		if _, err := NormalizeGroup(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
