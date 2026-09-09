package executor

import (
	"fmt"
	"strings"
)

// Replay the verified, atomic publication in the mock filesystem as well. Keep
// it strict: an upload must not succeed in tests if its staged bytes are absent.
func (m *Mock) publishUpload(cmd string) Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	for src, data := range m.fs {
		prefix := fmt.Sprintf("test \"$(wc -c < %s)\" -eq %d && chmod 600 %s && mv -- %s ", ShellQuote(src), len(data), ShellQuote(src), ShellQuote(src))
		if !strings.HasPrefix(cmd, prefix) {
			continue
		}
		word := strings.TrimPrefix(cmd, prefix)
		dest := word
		if strings.HasPrefix(word, "'") && strings.HasSuffix(word, "'") {
			dest = strings.ReplaceAll(word[1:len(word)-1], `'\''`, "'")
		}
		if ShellQuote(dest) != word {
			continue
		}
		m.fs[dest] = data
		delete(m.fs, src)
		return Result{}
	}
	return Result{RC: 1, Stderr: "upload size verification failed"}
}
