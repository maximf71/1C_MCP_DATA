package profile

import (
	"path/filepath"
	"testing"
)

func validProfile() Profile {
	return Profile{
		Version:          CurrentVersion,
		ID:               "demo",
		Name:             "Demo",
		BaseURL:          "http://127.0.0.1:8080/demo/hs/mcp-data/",
		MCPExecutable:    filepath.Join(`C:\`, "MCP1CData", "mcp-1c-data.exe"),
		CredentialFile:   filepath.Join(`C:\`, "MCP1CData", "profiles", "demo.credentials.bin"),
		Timeout:          "30s",
		MaxResponseBytes: 4 * 1024 * 1024,
	}
}

func TestProfileValidate(t *testing.T) {
	p := validProfile()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, baseURL := range []string{"https://127.0.0.1/x", "http://10.0.0.1/x", "file:///x"} {
		p.BaseURL = baseURL
		if err := p.Validate(); err == nil {
			t.Fatalf("expected %q to be rejected", baseURL)
		}
	}
}

func TestParseCredentialsDoesNotEchoPassword(t *testing.T) {
	_, err := ParseCredentials([]byte(`{"user":"","password":"highly-secret"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || got == "highly-secret" {
		t.Fatalf("unsafe error: %q", got)
	}
}
