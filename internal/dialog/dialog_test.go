package dialog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A stand-in for osascript: argument 4 is the question, 5 the button.
func fake(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "osascript")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := osascript
	osascript = path
	t.Cleanup(func() { osascript = original })
}

func TestTheClickConfirms(t *testing.T) {
	fake(t, `[ "$3" = "--" ] && [ "$4" = "Trash “X” in Y?" ] && [ "$5" = "Trash" ] && echo confirmed`)

	if yes, err := Confirm(context.Background(), "Trash “X” in Y?", "Trash"); !yes || err != nil {
		t.Fatalf("Confirm = %v, %v", yes, err)
	}
}

func TestCancelAndTimeoutRefuse(t *testing.T) {
	fake(t, `echo "execution error: User canceled. (-128)" >&2; exit 1`)
	if yes, err := Confirm(context.Background(), "Trash?", "Trash"); yes || err != nil {
		t.Fatalf("Cancel = %v, %v", yes, err)
	}

	fake(t, `echo timeout`)
	if yes, _ := Confirm(context.Background(), "Trash?", "Trash"); yes {
		t.Fatal("a timeout must refuse")
	}
}

func TestNoScreenRefuses(t *testing.T) {
	fake(t, `echo "execution error: No user interaction allowed. (-1713)" >&2; exit 1`)

	if yes, err := Confirm(context.Background(), "Trash?", "Trash"); yes || err == nil {
		t.Fatalf("no screen = %v, %v", yes, err)
	}
}
