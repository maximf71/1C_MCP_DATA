package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"

	"github.com/codex/mcp-1c-data/internal/profile"
)

var version = "dev"

func main() {
	profilePath := flag.String("profile", "", "absolute path to an MCP 1C Data profile")
	check := flag.Bool("check", false, "validate and decrypt the profile without starting MCP")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *profilePath == "" {
		log.Fatal("--profile is required")
	}
	p, err := profile.Load(*profilePath)
	if err != nil {
		log.Fatalf("profile validation failed: %v", err)
	}
	cipher, err := os.ReadFile(p.CredentialFile)
	if err != nil {
		log.Fatalf("credential profile cannot be read: %v", err)
	}
	plain, err := profile.UnprotectCurrentUser(cipher, p.ID)
	if err != nil {
		log.Fatalf("credential profile cannot be decrypted: %v", err)
	}
	credentials, err := profile.ParseCredentials(plain)
	for i := range plain {
		plain[i] = 0
	}
	if err != nil {
		log.Fatalf("credential profile is invalid: %v", err)
	}
	if _, err := os.Stat(p.MCPExecutable); err != nil {
		log.Fatalf("MCP executable is unavailable: %v", err)
	}
	if *check {
		result, _ := json.Marshal(map[string]any{"ok": true, "profile_id": p.ID, "profile_name": p.Name, "user_configured": true})
		fmt.Println(string(result))
		return
	}
	args := []string{"--base-url", p.BaseURL}
	if p.Timeout != "" {
		args = append(args, "--timeout", p.Timeout)
	}
	if p.MaxResponseBytes > 0 {
		args = append(args, "--max-response-bytes", strconv.FormatInt(p.MaxResponseBytes, 10))
	}
	cmd := exec.Command(p.MCPExecutable, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(),
		"MCP_1C_DATA_USER="+credentials.User,
		"MCP_1C_DATA_PASSWORD="+credentials.Password,
	)
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		log.Fatalf("cannot start MCP server: %v", err)
	}
}
