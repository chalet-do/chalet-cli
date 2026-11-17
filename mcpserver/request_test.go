package mcpserver

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/basecamp/mcp/catalog"
)

// A list in a query goes as Rails reads one, a name[] for each item, and a
// single value as itself: sent through scalar, a list reached the app as
// "[a b]", which it refused.
func TestAListInAQueryGoesAsRailsReadsIt(t *testing.T) {
	op := &catalog.Operation{Action: "feed", Path: "/activity", Params: []catalog.Param{
		{Name: "project_ids", In: "query"}, {Name: "before", In: "query"},
	}}

	_, query, _, err := buildRequest(op, map[string]any{"project_ids": []any{"a", "b"}, "before": "2026-11-17"})
	if err != nil {
		t.Fatal(err)
	}

	want := url.Values{"project_ids[]": {"a", "b"}, "before": {"2026-11-17"}}
	if !reflect.DeepEqual(query, want) {
		t.Errorf("query = %v, want %v", query, want)
	}
}
