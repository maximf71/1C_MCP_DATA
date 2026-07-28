package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const CurrentVersion = 1

type Profile struct {
	Version          int    `json:"version"`
	ID               string `json:"id"`
	Name             string `json:"name"`
	BaseURL          string `json:"base_url"`
	MCPExecutable    string `json:"mcp_executable"`
	CredentialFile   string `json:"credential_file"`
	Timeout          string `json:"timeout,omitempty"`
	MaxResponseBytes int64  `json:"max_response_bytes,omitempty"`
}

type Credentials struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

func Load(path string) (Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read profile: %w", err)
	}
	var result Profile
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Profile{}, fmt.Errorf("decode profile: %w", err)
	}
	if err := result.Validate(); err != nil {
		return Profile{}, err
	}
	return result, nil
}

func (p Profile) Validate() error {
	if p.Version != CurrentVersion {
		return fmt.Errorf("unsupported profile version %d", p.Version)
	}
	if strings.TrimSpace(p.ID) == "" || strings.ContainsAny(p.ID, "\\/\r\n\t") {
		return errors.New("profile id is invalid")
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return errors.New("base_url is invalid")
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "http" || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return errors.New("base_url must use HTTP on loopback")
	}
	if !filepath.IsAbs(p.MCPExecutable) || !filepath.IsAbs(p.CredentialFile) {
		return errors.New("mcp_executable and credential_file must be absolute paths")
	}
	if p.Timeout != "" {
		d, err := time.ParseDuration(p.Timeout)
		if err != nil || d <= 0 || d > 30*time.Second {
			return errors.New("timeout must be between 1ns and 30s")
		}
	}
	if p.MaxResponseBytes < 0 || p.MaxResponseBytes > 4*1024*1024 {
		return errors.New("max_response_bytes must be between 0 and 4 MiB")
	}
	return nil
}

func ParseCredentials(data []byte) (Credentials, error) {
	var result Credentials
	if err := json.Unmarshal(data, &result); err != nil {
		return Credentials{}, errors.New("decrypted credential data is invalid")
	}
	if strings.TrimSpace(result.User) == "" {
		return Credentials{}, errors.New("1C username is empty")
	}
	return result, nil
}
