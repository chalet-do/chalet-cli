package mcpserver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/basecamp/mcp/catalog"
	"github.com/basecamp/mcp/gateway"
)

// SupportedMajor is the catalog version this build understands. A newer
// major means the app changed the contract, and only a newer chalet can
// read it (api-spec §14.7).
const SupportedMajor = "1"

// Catalog is /agent_catalog.json: everything chalet-cli knows about Chalet.
// Every tool but chalet_picture, every summary and every path comes from
// here, which is why a feature the app declares reaches Claude with no
// chalet-cli release.
type Catalog struct {
	Version    string      `json:"version"`
	Rules      string      `json:"rules"`
	Domains    []Domain    `json:"domains"`
	Operations []Operation `json:"operations"`
}

// Domain is the app's own words for one area: the title and blurb its
// tools carry. The first one listed is the one an agent calls first.
type Domain struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Blurb string `json:"blurb"`
}

// Operation is one declared action of the app, joined to its route.
type Operation struct {
	Domain     string          `json:"domain"`
	Name       string          `json:"name"`
	Summary    string          `json:"summary"`
	Effect     string          `json:"effect"` // read, write or destructive
	Idempotent bool            `json:"idempotent"`
	Method     string          `json:"method"`
	Path       string          `json:"path"`
	Params     []catalog.Param `json:"params"`
	Body       map[string]any  `json:"body"`
	Paginated  bool            `json:"paginated"`
}

// ParseCatalog reads the app's catalog and refuses one this build cannot
// serve honestly.
func ParseCatalog(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("the app's catalog is not JSON: %w", err)
	}
	if major, _, _ := strings.Cut(c.Version, "."); major != SupportedMajor {
		return nil, fmt.Errorf("the app speaks catalog version %q and this chalet understands %s.x: upgrade chalet", c.Version, SupportedMajor)
	}
	for _, op := range c.Operations {
		switch op.Effect {
		case "read", "write", "destructive":
		default:
			return nil, fmt.Errorf("operation %s.%s has an unknown effect %q: upgrade chalet", op.Domain, op.Name, op.Effect)
		}
	}
	return &c, nil
}

// Narrow keeps the operations of the named domains and drops the rest, so a
// narrowed server's tools — the destructive one included — hold nothing from
// a domain nobody named. An unknown name stops the server instead of serving
// more or less than was asked for. No names keep everything.
func (c *Catalog) Narrow(names []string) (*Catalog, error) {
	if len(names) == 0 {
		return c, nil
	}

	known := map[string]bool{}
	for _, op := range c.Operations {
		known[op.Domain] = true
	}
	keep := map[string]bool{}
	for _, name := range names {
		if !known[name] {
			list := make([]string, 0, len(known))
			for key := range known {
				list = append(list, key)
			}
			sort.Strings(list)
			return nil, fmt.Errorf("unknown domain %q (known: %s)", name, strings.Join(list, ", "))
		}
		keep[name] = true
	}

	narrowed := *c
	narrowed.Operations = nil
	for _, op := range c.Operations {
		if keep[op.Domain] {
			narrowed.Operations = append(narrowed.Operations, op)
		}
	}
	return &narrowed, nil
}

// GatewayDomains groups the operations into tools by effect: a read tool per
// domain (chalet_todos), a write sibling (chalet_todos_write), and one
// chalet_destructive for every trash and archive — so each tool's
// annotations are true of everything in it (api-spec §14.4).
//
// The rules ride in the first read tool as well as in the server's
// instructions, because some clients drop instructions.
func (c *Catalog) GatewayDomains() ([]gateway.Domain, error) {
	reads, writes := map[string]*catalog.Domain{}, map[string]*catalog.Domain{}
	destructive := &catalog.Domain{
		Key:   "destructive",
		Tool:  "chalet_destructive",
		Title: "Chalet: trash and archive",
		Blurb: "Trash or archive anything in Chalet. Both can be undone. A call here only hands the owner Chalet's confirm page: nothing happens until they click there.",
	}

	for _, op := range c.Operations {
		entry := op.gatewayOperation()
		switch op.Effect {
		case "read":
			d := c.domain(reads, op.Domain, "")
			d.Operations = append(d.Operations, entry)
		case "write":
			d := c.domain(writes, op.Domain, "_write")
			d.Operations = append(d.Operations, entry)
		case "destructive":
			if _, taken := destructive.Operation(entry.Action); taken {
				return nil, fmt.Errorf("two destructive operations are both called %q: the app must name them apart", entry.Action)
			}
			destructive.Operations = append(destructive.Operations, entry)
		}
	}

	var domains []gateway.Domain
	rulesPlaced := c.Rules == ""
	for _, key := range c.order() {
		read, write := reads[key], writes[key]
		if read != nil && write != nil {
			read.Counterpart, write.Counterpart = write.Tool, read.Tool
		}
		if read != nil {
			if !rulesPlaced {
				read.Blurb += "\n\n" + strings.TrimSpace(c.Rules)
				rulesPlaced = true
			}
			domains = append(domains, sorted(read))
		}
		if write != nil {
			domains = append(domains, sorted(write))
		}
	}
	if len(destructive.Operations) > 0 {
		domains = append(domains, sorted(destructive))
	}
	return domains, nil
}

func (op Operation) gatewayOperation() *catalog.Operation {
	return &catalog.Operation{
		ID:          op.Domain + "." + op.Name,
		Action:      op.Name,
		Tag:         op.Domain,
		Method:      op.Method,
		Path:        op.Path,
		Summary:     op.Summary,
		ReadOnly:    op.Effect == "read",
		Idempotent:  op.Idempotent,
		Destructive: op.Effect == "destructive",
		Paginated:   op.Paginated,
		Params:      op.Params,
		Body:        op.Body,
	}
}

func (c *Catalog) domain(set map[string]*catalog.Domain, key, suffix string) *catalog.Domain {
	if d, ok := set[key]; ok {
		return d
	}
	title, blurb := key, ""
	for _, d := range c.Domains {
		if d.Key == key {
			title, blurb = d.Title, d.Blurb
		}
	}
	if suffix != "" {
		title += " (writes)"
	}
	d := &catalog.Domain{Key: key + suffix, Tool: "chalet_" + key + suffix, Title: "Chalet: " + title, Blurb: blurb}
	set[key] = d
	return d
}

// The app's order, then any domain it forgot to list, alphabetically.
func (c *Catalog) order() []string {
	var keys []string
	for _, d := range c.Domains {
		keys = appendOnce(keys, d.Key)
	}
	var rest []string
	for _, op := range c.Operations {
		if !contains(keys, op.Domain) {
			rest = appendOnce(rest, op.Domain)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

func sorted(d *catalog.Domain) *catalog.Domain {
	sort.Slice(d.Operations, func(i, j int) bool { return d.Operations[i].Action < d.Operations[j].Action })
	return d
}

func appendOnce(list []string, value string) []string {
	if contains(list, value) {
		return list
	}
	return append(list, value)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
