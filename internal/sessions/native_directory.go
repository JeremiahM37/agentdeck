package sessions

import "strings"

// nativeDirectoryArgs replaces an explicit Codex directory switch and appends a
// per-launch template. Arguments here are argv words, not shell fragments.
func nativeDirectoryArgs(args []string) []string {
	out := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--cd" || arg == "-C" {
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--cd=") || strings.HasPrefix(arg, "-C") {
			continue
		}
		out = append(out, arg)
	}
	return append(out, "--cd", "{dir}")
}
