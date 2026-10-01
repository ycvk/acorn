package main

import (
	"context"
	"fmt"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type EchoArgs struct {
	Text string `json:"text" jsonschema:"text to echo"`
}

type AddArgs struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type SampleArgs struct {
	Prompt string `json:"prompt" jsonschema:"prompt to send to the LLM"`
}

func echo(_ context.Context, _ *mcp.CallToolRequest, args EchoArgs) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + args.Text}}}, nil, nil
}

func add(_ context.Context, _ *mcp.CallToolRequest, args AddArgs) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%d", args.X+args.Y)}}}, nil, nil
}

// sample requests sampling via the multi round-trip pattern (SEP-2322): on
// protocol >= 2026-07-28 the server returns InputRequests and the client
// retries the call with the fulfilled InputResponses.
func sample(_ context.Context, req *mcp.CallToolRequest, args SampleArgs) (*mcp.CallToolResult, any, error) {
	if len(req.Params.InputResponses) == 0 {
		return &mcp.CallToolResult{
			InputRequests: mcp.InputRequestMap{"msg": &mcp.CreateMessageParams{
				Messages: []*mcp.SamplingMessage{
					{Role: mcp.Role("user"), Content: &mcp.TextContent{Text: args.Prompt}},
				},
			}},
		}, nil, nil
	}
	var text string
	switch res := req.Params.InputResponses["msg"].(type) {
	case *mcp.CreateMessageResult:
		text = textFromContent(res.Content)
	case *mcp.CreateMessageWithToolsResult:
		for _, c := range res.Content {
			text += textFromContent(c)
		}
	default:
		return nil, nil, fmt.Errorf("unexpected sampling response type %T", res)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "sampled: " + text}}}, nil, nil
}

func textFromContent(c mcp.Content) string {
	if tc, ok := c.(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture-server", Version: "v0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "echo text"}, echo)
	mcp.AddTool(server, &mcp.Tool{Name: "add", Description: "add numbers"}, add)
	mcp.AddTool(server, &mcp.Tool{Name: "sample", Description: "sample an LLM response"}, sample)

	// Add a resource so Resources() can be tested.
	server.AddResource(&mcp.Resource{
		Name:        "test-resource",
		URI:         "test://resource",
		Description: "A test resource",
		MIMEType:    "text/plain",
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      "test://resource",
				MIMEType: "text/plain",
				Text:     "hello from resource",
			}},
		}, nil
	})

	// Add a prompt so Prompts() can be tested.
	server.AddPrompt(&mcp.Prompt{
		Name:        "test-prompt",
		Description: "A test prompt",
	}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "A test prompt",
			Messages: []*mcp.PromptMessage{{
				Role: "user",
				Content: &mcp.TextContent{
					Text: "You are a test prompt.",
				},
			}},
		}, nil
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
