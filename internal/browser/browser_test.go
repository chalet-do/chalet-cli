package browser

import "testing"

// Only a web link reaches the opener; anything else is refused before it runs.
func TestOpensOnlyWebLinks(t *testing.T) {
	for _, link := range []string{"file:///etc/passwd", "/Applications/Calculator.app", "javascript:alert(1)", "-a Terminal"} {
		if err := Open(link); err == nil {
			t.Errorf("Open(%q) = nil, want a refusal", link)
		}
	}
}
