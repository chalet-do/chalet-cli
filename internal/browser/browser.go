// Package browser shows the owner a web page: the app's confirm page for a
// call that waits for their click.
package browser

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// Open hands a web link to the system's opener. Only http and https: the link
// comes from the app's answer, and the opener would as readily start a file or
// an application. An absolute path on macOS, because Claude Desktop may start
// chalet with an empty PATH.
func Open(link string) error {
	if u, err := url.Parse(link); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("not a web link: %q", link)
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("/usr/bin/open", link).Run()
	case "linux":
		return exec.Command("xdg-open", link).Run()
	case "windows":
		// Not cmd's start, which would read an & in the link as a second command.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", link).Run()
	default:
		return fmt.Errorf("no browser opener on %s", runtime.GOOS)
	}
}
