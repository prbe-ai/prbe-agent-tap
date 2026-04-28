package watch

import (
	"bytes"
	"encoding/json"
)

// SplitLines splits a buffer into complete newline-terminated lines.
// Returns the parsed lines, the byte count of any trailing partial line, and any error.
// Blank lines are skipped.
func SplitLines(buf []byte) ([][]byte, int, error) {
	var out [][]byte
	start := 0
	for i := 0; i < len(buf); i++ {
		if buf[i] == '\n' {
			line := bytes.TrimRight(buf[start:i], "\r")
			if len(line) > 0 {
				out = append(out, append([]byte(nil), line...))
			}
			start = i + 1
		}
	}
	return out, len(buf) - start, nil
}

// ValidateJSON returns nil if line is well-formed JSON.
func ValidateJSON(line []byte) error {
	var v interface{}
	return json.Unmarshal(line, &v)
}
