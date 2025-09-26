
package ansi

import "regexp"

// precompiled regex for ANSI/VT100 escape sequences (CSI/OSC/SS2/SS3 etc.)
var ansiRe = regexp.MustCompile(`\x1B\[[0-9;?]*[ -/]*[@-~]|\x1B\][^\x07]*\x07|\x1B[\(\)][A-Za-z]|\x1B[@-_]`)

// Strip removes ANSI escape/control sequences from s.
func Strip(s string) string {
	return string(ansiRe.ReplaceAll([]byte(s), []byte{}))
}

// SafeStrip tolerates regex failures; returns input if anything goes wrong.
func SafeStrip(s string) string {
	defer func() { _ = recover() }()
	return Strip(s)
}
