package mcpserver

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/basecamp/mcp/catalog"

	"github.com/chalet-do/chalet-cli/internal/chalet"
)

// uploader is an API that puts each file up as "signed:<path>".
type uploader struct{ uploaded []string }

func (u *uploader) Do(context.Context, string, string, url.Values, map[string]any, http.Header) (*chalet.Response, error) {
	return &chalet.Response{Status: http.StatusNoContent}, nil
}

func (u *uploader) Fetch(context.Context, string) (*chalet.Response, error) { return nil, nil }

func (u *uploader) Upload(_ context.Context, path string) (string, error) {
	u.uploaded = append(u.uploaded, path)
	return "signed:" + path, nil
}

// Any field whose items the catalog marks "upload": a full path goes up and
// its signed id takes its place; a signed id already goes as it came.
func TestAFieldOfFilesTakesLocalPaths(t *testing.T) {
	api := &uploader{}
	venue := filepath.Join(t.TempDir(), "Venue.pdf") // a full path on every system
	op := &catalog.Operation{Body: map[string]any{"properties": map[string]any{
		"signed_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string", "format": "upload"}},
		"title":      map[string]any{"type": "string"},
	}}}
	body := map[string]any{"signed_ids": []any{venue, "eyJfcmFpbHMi--abc"}, "title": "/not/a/file"}

	if err := (handler{api: api}).uploadFiles(context.Background(), op, body); err != nil {
		t.Fatal(err)
	}

	if want := []any{"signed:" + venue, "eyJfcmFpbHMi--abc"}; !reflect.DeepEqual(body["signed_ids"], want) {
		t.Fatalf("signed_ids = %v, want %v", body["signed_ids"], want)
	}
	if !reflect.DeepEqual(api.uploaded, []string{venue}) || body["title"] != "/not/a/file" {
		t.Fatalf("uploaded %v, title %v", api.uploaded, body["title"])
	}
}
