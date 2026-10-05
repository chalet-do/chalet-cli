// Package dialog asks the owner on their Mac before a call goes ahead that
// they must agree to: a trash, an archive, or a write that clients will see
// (api-spec §14.5). Annotations only set a client's default and a model can
// make two calls, so a click in a system dialog is the gate.
package dialog

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Absolute: Claude Desktop may start chalet with an empty PATH.
var osascript = "/usr/bin/osascript"

// GiveUp is how long the dialog waits for a click before it refuses.
var GiveUp = 60 * time.Second

// The question and the button arrive as arguments and never as script text,
// so a to-do's title cannot write AppleScript. Cancel is the default button:
// Return refuses. Anything but a click on the button — Cancel, Escape, the
// timeout, no screen to show on — answers no.
const script = `on run argv
	set question to item 1 of argv
	set agree to item 2 of argv
	set waiting to (item 3 of argv) as integer
	set answer to display dialog question with title "Chalet" buttons {"Cancel", agree} default button "Cancel" cancel button "Cancel" with icon caution giving up after waiting
	if gave up of answer then return "timeout"
	if button returned of answer is agree then return "confirmed"
	return "cancelled"
end run`

// Confirm shows the question and reports whether the owner clicked the button.
func Confirm(ctx context.Context, question, button string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, GiveUp+10*time.Second)
	defer cancel()

	seconds := fmt.Sprint(int(GiveUp.Seconds()))
	// "--" ends osascript's options: a question that starts with a dash is
	// still the question.
	out, err := exec.CommandContext(ctx, osascript, "-e", script, "--", question, button, seconds).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && strings.Contains(string(exit.Stderr), "(-128)") {
			return false, nil // Cancel
		}
		return false, fmt.Errorf("the confirmation dialog could not open: %w", err)
	}
	return strings.TrimSpace(string(out)) == "confirmed", nil
}
