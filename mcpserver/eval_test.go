package mcpserver

import (
	"context"
	"os"
	"testing"

	"github.com/basecamp/mcp/eval"
	"github.com/basecamp/mcp/gateway"
	"github.com/basecamp/mcp/mcptest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAgentEval scores how well a model drives chalet's tools, with the MCP
// library's eval: requests in plain words are made from the tools and their
// describe, and a model's {tool, action, params} for each is graded by rule.
// The tools are the recorded catalog's, served in process — no Chalet, no
// token, and the handler never runs.
//
// The oracle answers every request with the right call, for nothing, so the
// harness itself is checked on every run. A model is scored only when
// CHALET_EVAL_MODEL names one (an Anthropic model id), with ANTHROPIC_API_KEY
// set; its report is the test's log, and a low score fails nothing.
func TestAgentEval(t *testing.T) {
	model := os.Getenv("CHALET_EVAL_MODEL")
	if model != "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("CHALET_EVAL_MODEL is set, but ANTHROPIC_API_KEY is not: the model run needs it")
	}

	domains, err := recordedCatalog(t).GatewayDomains()
	if err != nil {
		t.Fatal(err)
	}
	gw, err := gateway.New(domains, gateway.Config{
		Handler: func(context.Context, gateway.Domain, gateway.Operation, map[string]any) (*mcp.CallToolResult, error) {
			t.Error("the eval only lists and describes: no call may run")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session := mcptest.Connect(t, gw.BuildMCPServer(&mcp.Implementation{Name: "chalet-cli", Version: "eval"}, nil))

	specs, err := eval.SpecFromSession(t.Context(), session)
	if err != nil {
		t.Fatal(err)
	}
	scenarios := eval.Generate(specs, eval.GenerateOptions{N: 12, Seed: 1})

	models := []eval.Model{eval.NewOracleModel(scenarios)}
	if model != "" {
		m, err := eval.NewAPIModel(model, model)
		if err != nil {
			t.Fatal(err)
		}
		models = append(models, m)
	}

	report, err := eval.Run(t.Context(), session, eval.Config{Models: models, Scenarios: scenarios})
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + report.Render("chalet"))

	var oracle []eval.Record
	for _, r := range report.Records {
		if r.Model == "oracle" {
			oracle = append(oracle, r)
		}
	}
	if err := eval.RequirePass(oracle); err != nil {
		t.Fatalf("the right call must pass every request: %v", err)
	}
}
