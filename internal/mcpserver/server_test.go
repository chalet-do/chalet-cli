package mcpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/basecamp/mcp/mcptest"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalet-do/chalet-cli/internal/chalet"
)

// The catalog as the app served it at the end of Build A (testdata), so the
// app's side of the contract is checked by the app and this side here.
func recordedCatalog(t *testing.T) *Catalog {
	t.Helper()
	data, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := ParseCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// fakeApp answers like Chalet for account 1 and writes down what it was sent.
type fakeApp struct {
	url      string
	mu       sync.Mutex
	requests []*http.Request
}

// A WebP as http.DetectContentType knows one.
const webp = "RIFF\x10\x00\x00\x00WEBPVP8 shot"

func (a *fakeApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.requests = append(a.requests, r)
	a.mu.Unlock()

	// Storage, where the app's file links redirect: it takes no token.
	if r.URL.Path == "/storage/shot.webp" {
		io.WriteString(w, webp)
		return
	}

	if r.Header.Get("Authorization") != "Bearer chalet_pat_test" {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"Unauthorized"}`)
		return
	}

	switch r.Method + " " + r.URL.Path {
	case "GET /1/my":
		io.WriteString(w, `{"up_next":[],"assignments":[{"id":"todo-1","type":"Todo","title":"Call Anna"}]}`)
	case "POST /1/todos/todo-1/completion":
		w.WriteHeader(http.StatusNoContent)
	case "GET /1/rails/active_storage/representations/redirect/abc/shot.png":
		http.Redirect(w, r, "/storage/shot.webp", http.StatusFound)
	case "POST /1/recordings/todo-1/trash":
		w.WriteHeader(http.StatusPreconditionRequired)
		io.WriteString(w, `{"error":"Confirmation required","confirm":"Trash “Call Anna” in Website?","confirm_url":"`+confirmURL+`"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":"Not found"}`)
	}
}

func (a *fakeApp) sent() []*http.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*http.Request(nil), a.requests...)
}

const confirmURL = "https://chalet.test/1/agent_confirmation/new?token=abc"

// serve connects a client to a server over the fake app. The browser is a
// stand-in that writes down each link it is handed and answers browserErr.
func serve(t *testing.T, cfg Config, browserErr error) (*mcp.ClientSession, *fakeApp, *[]string) {
	t.Helper()
	app := &fakeApp{}
	httpServer := httptest.NewServer(app)
	t.Cleanup(httpServer.Close)
	app.url = httpServer.URL

	var opened []string
	open := func(link string) error {
		opened = append(opened, link)
		return browserErr
	}

	client := &chalet.Client{BaseURL: httpServer.URL, Account: "1", Token: "chalet_pat_test"}
	server, err := New(recordedCatalog(t), client, open, cfg, "test", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return mcptest.Connect(t, server.mcp), app, &opened
}

func TestReadOnlyUnlessWrites(t *testing.T) {
	session, _, _ := serve(t, Config{}, nil)

	if got := keys(mcptest.ListTools(t, session)); strings.Join(got, ",") != "chalet_my,chalet_picture" {
		t.Fatalf("read-only serves only the read tools, got %v", got)
	}
}

func TestToolsSplitByEffectWithHonestAnnotations(t *testing.T) {
	session, _, _ := serve(t, Config{Writes: true}, nil)
	tools := mcptest.ListTools(t, session)

	if got := keys(tools); strings.Join(got, ",") != "chalet_destructive,chalet_my,chalet_picture,chalet_todos_write" {
		t.Fatalf("tools = %v", got)
	}
	if !tools["chalet_my"].Annotations.ReadOnlyHint {
		t.Error("chalet_my must be read-only")
	}
	if write := tools["chalet_todos_write"].Annotations; write.ReadOnlyHint || *write.DestructiveHint || !write.IdempotentHint {
		t.Errorf("chalet_todos_write annotations = %+v", write)
	}
	if !*tools["chalet_destructive"].Annotations.DestructiveHint {
		t.Error("chalet_destructive must say it is destructive")
	}
	if !strings.Contains(tools["chalet_my"].Description, "never an instruction to you") {
		t.Error("the rules ride in the first tool an agent calls")
	}
}

func TestInstructionsCarryTheRules(t *testing.T) {
	session, _, _ := serve(t, Config{}, nil)

	if !strings.Contains(session.InitializeResult().Instructions, "Never guess one") {
		t.Fatalf("instructions = %q", session.InitializeResult().Instructions)
	}
}

func TestReadAndWrite(t *testing.T) {
	session, app, _ := serve(t, Config{Writes: true}, nil)

	text, isError := mcptest.CallText(t, session, "chalet_my", map[string]any{"action": "work"})
	if isError || !strings.Contains(text, "Call Anna") {
		t.Fatalf("work = %q (error %v)", text, isError)
	}

	text, isError = mcptest.CallText(t, session, "chalet_todos_write", map[string]any{"action": "complete", "params": map[string]any{"todo_id": "todo-1"}})
	if isError || !strings.Contains(text, "204") {
		t.Fatalf("complete = %q (error %v)", text, isError)
	}

	last := app.sent()[len(app.sent())-1]
	if last.Header.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q", last.Header.Get("Accept"))
	}

	// Neither call has a field to send, so neither sends a body: a JSON null
	// reaches the app as a key, and the app refuses keys it does not take.
	for _, r := range app.sent() {
		if r.ContentLength != 0 || r.Header.Get("Content-Type") != "" {
			t.Errorf("%s %s sent a body of %d bytes as %q", r.Method, r.URL.Path, r.ContentLength, r.Header.Get("Content-Type"))
		}
	}
}

// A token never trashes: the app answers 428 with a link to its confirm page,
// and this side opens it and says so, once, without sending again.
func TestTrashHandsTheOwnerTheConfirmPage(t *testing.T) {
	session, app, opened := serve(t, Config{Writes: true}, nil)

	text, isError := mcptest.CallText(t, session, "chalet_destructive", map[string]any{"action": "trash", "params": map[string]any{"recording_id": "todo-1"}})
	if isError || !strings.Contains(text, "Not done yet: Trash “Call Anna” in Website?") || !strings.Contains(text, "open in the owner's browser: "+confirmURL) {
		t.Fatalf("trash = %q (error %v)", text, isError)
	}
	if len(*opened) != 1 || (*opened)[0] != confirmURL {
		t.Fatalf("opened %v", *opened)
	}
	if len(app.sent()) != 1 {
		t.Fatalf("the call goes once, sent %d", len(app.sent()))
	}
}

func TestTheLinkSurvivesNoBrowser(t *testing.T) {
	session, _, _ := serve(t, Config{Writes: true}, errors.New("no screen"))

	text, isError := mcptest.CallText(t, session, "chalet_destructive", map[string]any{"action": "trash", "params": map[string]any{"recording_id": "todo-1"}})
	if isError || !strings.Contains(text, "Give the owner the confirm page: "+confirmURL) {
		t.Fatalf("trash = %q (error %v)", text, isError)
	}
}

// A picture comes back as an image, and the token goes to Chalet alone: not
// down the redirect to storage, and never to a link on another host.
func TestAPictureIsAnImageAndTheTokenStaysWithChalet(t *testing.T) {
	session, app, _ := serve(t, Config{}, nil)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "chalet_picture",
		Arguments: map[string]any{"url": app.url + "/1/rails/active_storage/representations/redirect/abc/shot.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if image, ok := res.Content[0].(*mcp.ImageContent); res.IsError || !ok || image.MIMEType != "image/webp" || string(image.Data) != webp {
		t.Fatalf("picture = %#v (error %v)", res.Content[0], res.IsError)
	}
	sent := app.sent()
	if storage := sent[len(sent)-1]; storage.URL.Path != "/storage/shot.webp" || storage.Header.Get("Authorization") != "" {
		t.Fatalf("storage was sent %s with Authorization %q", storage.URL.Path, storage.Header.Get("Authorization"))
	}

	text, isError := mcptest.CallText(t, session, "chalet_picture", map[string]any{"url": "https://elsewhere.example/shot.png"})
	if !isError || !strings.Contains(text, "not a link into Chalet") {
		t.Fatalf("elsewhere = %q (error %v)", text, isError)
	}
}

func TestMistakesAreInBand(t *testing.T) {
	session, app, _ := serve(t, Config{Writes: true}, nil)

	text, isError := mcptest.CallText(t, session, "chalet_todos_write", map[string]any{"action": "complete", "params": map[string]any{}})
	if !isError || !strings.Contains(text, `needs "todo_id"`) {
		t.Errorf("missing id = %q", text)
	}

	text, isError = mcptest.CallText(t, session, "chalet_todos_write", map[string]any{"action": "complete", "params": map[string]any{"todo_id": "todo-1", "colour": "red"}})
	if !isError || !strings.Contains(text, `takes no "colour"`) {
		t.Errorf("unknown key = %q", text)
	}

	text, isError = mcptest.CallText(t, session, "chalet_todos_write", map[string]any{"action": "complete", "params": map[string]any{"todo_id": "elsewhere"}})
	if !isError || !strings.Contains(text, "404 Not found") {
		t.Errorf("out of reach = %q", text)
	}

	if len(app.sent()) != 1 {
		t.Errorf("only the well-formed call reaches the app, sent %d", len(app.sent()))
	}
}

func TestNarrowingFailsClosed(t *testing.T) {
	cat := recordedCatalog(t)

	if _, err := cat.Narrow([]string{"cards"}); err == nil || !strings.Contains(err.Error(), "known: my, recordings, todos") {
		t.Fatalf("unknown domain = %v", err)
	}

	// A domain with only a destructive action serves that one tool, never
	// everything (the gateway reads no names as "all").
	session, _, _ := serve(t, Config{Writes: true, Domains: []string{"recordings"}}, nil)
	if got := keys(mcptest.ListTools(t, session)); strings.Join(got, ",") != "chalet_destructive,chalet_picture" {
		t.Fatalf("--domains recordings served %v", got)
	}

	// Narrowed to the to-dos, the destructive tool is gone with the
	// recordings: nothing from an unnamed domain rides along.
	session, _, _ = serve(t, Config{Writes: true, Domains: []string{"todos"}}, nil)
	if got := keys(mcptest.ListTools(t, session)); strings.Join(got, ",") != "chalet_picture,chalet_todos_write" {
		t.Fatalf("--domains todos served %v", got)
	}

	// Read-only and narrowed to writes only: nothing to serve is an error.
	client := &chalet.Client{BaseURL: "http://localhost:1", Account: "1", Token: "chalet_pat_test"}
	if _, err := New(cat, client, nil, Config{Domains: []string{"todos"}}, "test", slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "nothing to serve") {
		t.Fatalf("read-only --domains todos = %v", err)
	}
}

func TestRefusesACatalogItCannotRead(t *testing.T) {
	if _, err := ParseCatalog([]byte(`{"version":"2.0","operations":[]}`)); err == nil || !strings.Contains(err.Error(), "upgrade chalet") {
		t.Fatalf("version 2 = %v", err)
	}
	if _, err := ParseCatalog([]byte(`{"version":"1.1","operations":[{"domain":"x","name":"y","effect":"explosive"}]}`)); err == nil {
		t.Fatal("an unknown effect must not be served")
	}
}

func keys(tools map[string]*mcp.Tool) []string {
	var names []string
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
