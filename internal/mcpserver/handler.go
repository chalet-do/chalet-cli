package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/basecamp/mcp/catalog"
	"github.com/basecamp/mcp/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chalet-do/chalet-cli/internal/chalet"
)

// API is the slice of chalet.Client the handler drives.
type API interface {
	Do(ctx context.Context, method, path string, query url.Values, body map[string]any, header http.Header) (*chalet.Response, error)
	Fetch(ctx context.Context, link string) (*chalet.Response, error)
	Upload(ctx context.Context, path string) (string, error)
}

// Opener shows the owner a web page: the app's confirm page for a call that
// waits for their click.
type Opener func(link string) error

type handler struct {
	api    API
	open   Opener
	logger *slog.Logger
}

// handle turns one tool call into one request to the app. The calling
// convention: path parameters by name, query parameters by name, and
// everything else is the JSON body. Failures are in-band error results.
func (h handler) handle(ctx context.Context, dom gateway.Domain, op gateway.Operation, params map[string]any) (*mcp.CallToolResult, error) {
	domain, ok := dom.(*catalog.Domain)
	if !ok {
		return failed(nil, fmt.Errorf("internal error: domain %q is not a catalog domain", dom.Name()), false), nil
	}
	operation, ok := domain.Operation(op.Action)
	if !ok {
		return failed(nil, fmt.Errorf("internal error: no action %q in %q", op.Action, dom.Name()), false), nil
	}
	write := !operation.ReadOnly

	path, query, body, err := buildRequest(operation, params)
	if err != nil {
		return failed(nil, err, write), nil
	}
	if err := h.uploadFiles(ctx, operation, body); err != nil {
		return failed(nil, err, write), nil
	}

	resp, err := h.api.Do(ctx, operation.Method, path, query, body, nil)
	if err != nil {
		return failed(nil, err, write), nil
	}

	// A token never runs a trash or an archive: the app answers 428 with its
	// question and a link to its confirm page, and the owner's click there does
	// it. This side only opens the link and says so.
	if resp.Status == http.StatusPreconditionRequired {
		return h.handOver(resp), nil
	}

	return h.result(resp, write)
}

func (h handler) handOver(resp *chalet.Response) *mcp.CallToolResult {
	var ask struct {
		Confirm    string `json:"confirm"`
		ConfirmURL string `json:"confirm_url"`
	}
	if err := json.Unmarshal(resp.Body, &ask); err != nil || ask.ConfirmURL == "" {
		return failed(resp, nil, true)
	}

	where := "The confirm page is open in the owner's browser"
	if err := h.open(ask.ConfirmURL); err != nil {
		h.logger.Warn("could not open the browser", "error", err)
		where = "Give the owner the confirm page"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
		"Not done yet: %s %s: %s — nothing changes until they click there. Do not call this again.",
		ask.Confirm, where, ask.ConfirmURL)}}}
}

// A bodiless answer says its status, a list with more pages says where the
// next one starts, and a refusal is an error result in the app's own words.
// A create needs nothing more: the app answers it with the record (Fizzy's
// render :show, status: :created).
func (h handler) result(resp *chalet.Response, write bool) (*mcp.CallToolResult, error) {
	if !resp.OK() {
		return failed(resp, nil, write), nil
	}
	if len(strings.TrimSpace(string(resp.Body))) == 0 {
		return gateway.JSONResult(map[string]any{"status": resp.Status})
	}
	if page := nextPage(resp.Header); page != "" {
		return gateway.JSONResult(map[string]any{"next_page": page, "results": json.RawMessage(resp.Body)})
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(resp.Body)}}}, nil
}

func buildRequest(op *catalog.Operation, params map[string]any) (string, url.Values, map[string]any, error) {
	rest := make(map[string]any, len(params))
	for name, value := range params {
		rest[name] = value
	}

	path, query := op.Path, url.Values{}
	for _, param := range op.Params {
		value, given := rest[param.Name]
		if !given {
			if param.Required {
				return "", nil, nil, fmt.Errorf("%s needs %q (call describe for the schema)", op.Action, param.Name)
			}
			continue
		}
		delete(rest, param.Name)

		switch param.In {
		case "path":
			path = strings.ReplaceAll(path, "{"+param.Name+"}", url.PathEscape(scalar(value)))
		case "query":
			query.Set(param.Name, scalar(value))
		}
	}

	if len(rest) == 0 {
		return path, query, nil, nil
	}
	if op.Body == nil {
		return "", nil, nil, fmt.Errorf("%s takes no %s (call describe for the schema)", op.Action, names(rest))
	}
	if properties, ok := op.Body["properties"].(map[string]any); ok {
		for name := range rest {
			if _, known := properties[name]; !known {
				return "", nil, nil, fmt.Errorf("%s has no field %q; it takes %s", op.Action, name, names(properties))
			}
		}
	}
	return path, query, rest, nil
}

// A field the catalog marks as files (its items' format is "upload") takes
// local files: each full path is put up first (chalet.Client.Upload) and its
// signed id goes in its place. Whatever is not a full path — a signed id
// already — goes as it came. The same for every operation with such a field,
// so a field of files the app opens tomorrow needs no release here.
func (h handler) uploadFiles(ctx context.Context, op *catalog.Operation, body map[string]any) error {
	properties, _ := op.Body["properties"].(map[string]any)
	for name, value := range body {
		schema, _ := properties[name].(map[string]any)
		items, _ := schema["items"].(map[string]any)
		if items["format"] != "upload" {
			continue
		}

		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s takes a list of files", name)
		}
		for i, item := range list {
			path, ok := item.(string)
			if !ok || !filepath.IsAbs(path) {
				continue
			}
			signedID, err := h.api.Upload(ctx, path)
			if err != nil {
				return err
			}
			list[i] = signedID
		}
	}
	return nil
}

// Ids and numbers alike travel as the plain text a URL carries.
func scalar(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// geared_pagination's Link header: rel="next" carries the next page number.
func nextPage(header http.Header) string {
	for _, link := range header.Values("Link") {
		for part := range strings.SplitSeq(link, ",") {
			if !strings.Contains(part, `rel="next"`) {
				continue
			}
			start, end := strings.Index(part, "<"), strings.Index(part, ">")
			if start < 0 || end <= start+1 {
				continue
			}
			if u, err := url.Parse(part[start+1 : end]); err == nil {
				return u.Query().Get("page")
			}
		}
	}
	return ""
}

// failed is every error a tool answers, as one small JSON object an agent can
// branch on: the sentence and its hint for the owner, a stable code, and
// whether the same call may go again. A call that got no answer, and Chalet's
// 429, 502, 503 and 504, may — though chalet itself never sends a write twice
// (chalet.Client.Do), so for a write the hint says to look first. An error
// that is not Chalet's answer nor the network's is this side's own refusal of
// the call: bad_request.
func failed(resp *chalet.Response, err error, write bool) *mcp.CallToolResult {
	answer := struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
		Hint      string `json:"hint,omitempty"`
	}{Code: "bad_request"}

	var unreachable *url.Error
	switch {
	case errors.As(err, &unreachable):
		answer.Error, answer.Code, answer.Retryable = err.Error(), "unavailable", true
	case err != nil:
		answer.Error = err.Error()
	default:
		answer.Error = "Chalet answered " + resp.Error()
		answer.Code, answer.Retryable, answer.Hint = refusal(resp)
	}
	if answer.Retryable && write {
		answer.Hint = strings.TrimSpace(answer.Hint + " chalet never sends a write twice: check whether it was done before you call again.")
	}

	data, _ := json.Marshal(answer)
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
}

func refusal(resp *chalet.Response) (code string, retryable bool, hint string) {
	switch resp.Status {
	case http.StatusBadRequest:
		return "bad_request", false, ""
	case http.StatusUnauthorized:
		return "unauthorized", false, "The token is wrong, revoked, or may only read. The owner can make a new one in Chalet's settings and run `chalet auth login`."
	case http.StatusForbidden:
		return "forbidden", false, "The owner may not do this, or it is not open to agents."
	case http.StatusNotFound:
		return "not_found", false, "The id is wrong, the thing is gone, or the owner cannot reach it."
	case http.StatusUnprocessableEntity:
		return "invalid", false, string(resp.Body)
	case http.StatusTooManyRequests:
		return "rate_limited", true, "Too many calls: wait a little before the next."
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "unavailable", true, ""
	}
	if resp.Status >= 500 {
		return "unavailable", false, ""
	}
	return "refused", false, ""
}

func names[V any](set map[string]V) string {
	list := make([]string, 0, len(set))
	for name := range set {
		list = append(list, strconv.Quote(name))
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}
