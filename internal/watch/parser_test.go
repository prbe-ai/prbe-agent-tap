package watch

import (
	"bytes"
	"strings"
	"testing"
)

func TestSplitLinesCompleteOnly(t *testing.T) {
	in := []byte(`{"a":1}` + "\n" + `{"b":2}` + "\n")
	lines, rem, err := SplitLines(in)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Fatalf("rem = %d want 0", rem)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines want 2", len(lines))
	}
	if !bytes.Equal(lines[0], []byte(`{"a":1}`)) {
		t.Fatalf("line 0 = %q", lines[0])
	}
}

func TestSplitLinesIncompleteFinal(t *testing.T) {
	in := []byte(`{"a":1}` + "\n" + `{"b":`)
	lines, rem, err := SplitLines(in)
	if err != nil {
		t.Fatal(err)
	}
	if rem != len(`{"b":`) {
		t.Fatalf("rem = %d want %d", rem, len(`{"b":`))
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1", len(lines))
	}
}

func TestSplitLinesSkipsBlanks(t *testing.T) {
	in := []byte("\n\n" + `{"a":1}` + "\n\n")
	lines, _, _ := SplitLines(in)
	if len(lines) != 1 {
		t.Fatalf("got %d lines want 1", len(lines))
	}
}

func TestValidateJSON(t *testing.T) {
	if err := ValidateJSON([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("valid line rejected: %v", err)
	}
	if err := ValidateJSON([]byte(`{`)); err == nil {
		t.Fatal("malformed line accepted")
	}
	if err := ValidateJSON([]byte(`not even json`)); err == nil {
		t.Fatal("non-json accepted")
	}
}

func TestValidateJSONLargeOK(t *testing.T) {
	big := []byte(`{"x":"` + strings.Repeat("a", 1024*1024) + `"}`)
	if err := ValidateJSON(big); err != nil {
		t.Fatalf("large line rejected: %v", err)
	}
}
