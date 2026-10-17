// Command mcp_server is a stdio MCP server for tests of skill packages'
// servers. It needs FIXTURE_TOKEN, serves an echo tool for every name in
// tools.txt in its working directory, and tools that report its working
// directory, its process id and the token, and one that breaks its stdio
// session while the process lives on.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoArgs struct {
	Text string `json:"text"`
}

type noArgs struct{}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func main() {
	token := os.Getenv("FIXTURE_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "FIXTURE_TOKEN is not set")
		os.Exit(3)
	}
	names, err := os.ReadFile("tools.txt")
	if err != nil {
		log.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	for _, name := range strings.Fields(string(names)) {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "Echo the text."}, func(_ context.Context, _ *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
			return text(name + ": " + args.Text), nil, nil
		})
	}
	mcp.AddTool(server, &mcp.Tool{Name: "cwd", Description: "The working directory."}, func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, any, error) {
		return text(cwd), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "pid", Description: "The process id."}, func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, any, error) {
		return text(fmt.Sprint(os.Getpid())), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "token", Description: "The token."}, func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, any, error) {
		return text(token), nil, nil
	})
	var hang atomic.Bool
	mcp.AddTool(server, &mcp.Tool{Name: "garble", Description: "Write a line that is not JSON to stdout and keep running after stdin closes."}, func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, any, error) {
		hang.Store(true)
		fmt.Println("added 32 packages in 2s")
		return text("garbled"), nil, nil
	})
	fmt.Fprintln(os.Stderr, "serving", strings.Join(strings.Fields(string(names)), ","))
	err = server.Run(context.Background(), &mcp.StdioTransport{})
	if hang.Load() {
		select {}
	}
	if err != nil {
		log.Fatal(err)
	}
}
