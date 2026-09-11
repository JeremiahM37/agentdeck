package console

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientAuthenticationErrorsAndUpload(t *testing.T) {
	content := []byte("pdf bytes\x00\xff")
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing bearer token")
		}
		switch r.URL.Path {
		case "/api/sessions":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"id":1}]`)
		case "/api/tasks/4/attachments":
			f, h, e := r.FormFile("file")
			if e != nil {
				t.Error(e)
				return
			}
			defer f.Close()
			b, _ := io.ReadAll(f)
			if !bytes.Equal(b, content) || h.Filename != "requirements.pdf" {
				t.Error("upload did not preserve bytes/name")
			}
			io.WriteString(w, `{"path":"/remote/requirements.pdf"}`)
		default:
			w.WriteHeader(409)
			io.WriteString(w, `{"detail":"already taken over"}`)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "test-token")
	if _, e := c.JSON("GET", "/sessions", nil); e != nil {
		t.Fatal(e)
	}
	if _, e := c.JSON("POST", "/api/tasks/1/takeover", nil); e == nil || e.Error() != "already taken over" {
		t.Fatalf("error: %v", e)
	}
	path := filepath.Join(t.TempDir(), "requirements.pdf")
	os.WriteFile(path, content, 0600)
	if _, e := c.Upload("task", "4", path); e != nil {
		t.Fatal(e)
	}
	if _, e := c.JSON("GET", "//attacker.example/path", nil); e == nil {
		t.Fatal("accepted external path")
	}
	if requests != 3 {
		t.Fatalf("%d requests", requests)
	}
}

func TestPlainConsoleRendersAPIObjectsAsReadableFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"ok","provider_extra":{"region":"west"}}`)
	}))
	defer srv.Close()
	var out bytes.Buffer
	u := NewUI(New(srv.URL, ""), strings.NewReader(""), &out, nil)
	if err := u.request("GET", "/health", nil); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.ContainsAny(got, "{}\"") || !strings.Contains(got, "Provider Extra") || !strings.Contains(got, "Region: west") {
		t.Fatalf("plain console emitted raw or incomplete data: %q", got)
	}
}
func TestDownloadPreservesExistingAndBinary(t *testing.T) {
	payload := []byte{0, 255, 27, 10}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	defer srv.Close()
	c := New(srv.URL, "")
	path := filepath.Join(t.TempDir(), "artifact.bin")
	if e := c.Download("/term/session/1/file?path=artifact.bin", path); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, payload) {
		t.Fatal("bytes changed")
	}
	if e := c.Download("/term/session/1/file", path); e == nil {
		t.Fatal("overwrote existing file")
	}
}
func TestConsoleApprovesThenReturnsToList(t *testing.T) {
	decided := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			b, _ := io.ReadAll(r.Body)
			if string(b) != `{"decision":"approved"}` || r.URL.Path != "/api/approvals/7/decision" {
				t.Errorf("wrong decision %s %s", r.URL.Path, b)
			}
			decided = true
			io.WriteString(w, `{"id":7}`)
			return
		}
		io.WriteString(w, `[{"id":7,"tool_name":"shell","status":"pending"}]`)
	}))
	defer srv.Close()
	var out bytes.Buffer
	ui := NewUI(New(srv.URL, ""), strings.NewReader("6\n7\nallow\nb\nb\nq\n"), &out, nil)
	if e := ui.Run(); e != nil {
		t.Fatal(e)
	}
	if !decided {
		t.Fatal("approval not sent")
	}
	if !strings.Contains(out.String(), "Action completed.") {
		t.Fatal(out.String())
	}
}
