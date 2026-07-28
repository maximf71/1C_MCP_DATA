package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/codex/mcp-1c-data/internal/domain"
	"github.com/codex/mcp-1c-data/internal/onec"
	mcpserver "github.com/codex/mcp-1c-data/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

func main() {
	baseURL := flag.String("base-url", "", "1C HTTP service base URL; HTTP is accepted only on loopback")
	userEnv := flag.String("user-env", "MCP_1C_DATA_USER", "environment variable containing the 1C username")
	passwordEnv := flag.String("password-env", "MCP_1C_DATA_PASSWORD", "environment variable containing the 1C password")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request timeout, maximum 30s")
	maxResponse := flag.Int64("max-response-bytes", domain.MaxResponseSize, "maximum response size, up to 4 MiB")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *baseURL == "" {
		log.Fatal("--base-url is required")
	}
	client, err := onec.NewClient(
		*baseURL,
		os.Getenv(*userEnv),
		os.Getenv(*passwordEnv),
		*timeout,
		*maxResponse,
	)
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	server := mcpserver.NewVersioned(client, version)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("MCP server stopped: %v", err)
	}
}
