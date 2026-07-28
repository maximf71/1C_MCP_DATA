package onec

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex/mcp-1c-data/internal/domain"
)

func TestNewClientURLPolicy(t *testing.T) {
	valid := []string{
		"http://localhost/base/hs/mcp-data/",
		"http://127.0.0.1/base/hs/mcp-data/",
		"http://[::1]/base/hs/mcp-data/",
		"https://onec.example.test/base/hs/mcp-data/",
	}
	for _, value := range valid {
		if _, err := NewClient(value, "", "", time.Second, domain.MaxResponseSize); err != nil {
			t.Errorf("NewClient(%q) error = %v", value, err)
		}
	}
	invalid := []string{
		"http://onec.example.test/base/",
		"ftp://localhost/base/",
		"http://user:secret@localhost/base/",
		"localhost/base/",
	}
	for _, value := range invalid {
		if _, err := NewClient(value, "", "", time.Second, domain.MaxResponseSize); err == nil {
			t.Errorf("NewClient(%q) succeeded", value)
		}
	}
}

func TestClientKeepsBasePathAndDoesNotExposeErrorBody(t *testing.T) {
	var gotPath, gotUser, gotPassword string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		gotUser, gotPassword, _ = request.BasicAuth()
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusBadRequest)
		_, _ = response.Write([]byte(`{"error":{"code":"QUERY_EXECUTION_FAILED","message":"secret row and stack trace"}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/publication/hs/mcp-data/", "reader", "not-logged", time.Second, domain.MaxResponseSize)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetConfigurationInfo(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if gotPath != "/publication/hs/mcp-data/info" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotUser != "reader" || gotPassword != "not-logged" {
		t.Fatal("basic authentication was not sent")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "stack") || strings.Contains(err.Error(), "not-logged") {
		t.Fatalf("sensitive response was exposed: %v", err)
	}
}

func TestClientEnforcesResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", 65)))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/", "", "", time.Second, 64)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetConfigurationInfo(context.Background())
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestClientDecodesLatestDocuments(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		gotPath, gotMethod = request.URL.Path, request.Method
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"documents":[{"metadata":"Document.Test","presentation":"Test 1","uuid":"00000000-0000-0000-0000-000000000001","number":"1","date":"2026-07-26T12:00:00","posted":false,"deletion_mark":false}],"max_date":"2026-07-26T12:00:00","returned_count":1,"truncated":false,"readable_document_types":2,"scanned_document_types":2,"complete":true,"filters":["ПометкаУдаления = Ложь"],"execution_ms":10,"data_is_untrusted":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/publication/hs/mcp-data/", "", "", time.Second, domain.MaxResponseSize)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.GetLatestDocuments(context.Background(), domain.LatestDocumentsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/publication/hs/mcp-data/latest-documents" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if len(result.Documents) != 1 || result.Documents[0].Metadata != "Document.Test" || !result.Complete {
		t.Fatalf("result = %#v", result)
	}
}

func TestClientPostsFilteredLatestDocuments(t *testing.T) {
	var gotMethod string
	var got domain.LatestDocumentsRequest
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		gotMethod = request.Method
		if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		_, _ = response.Write([]byte(`{"documents":[],"returned_count":0,"readable_document_types":1,"scanned_document_types":1,"complete":true,"filters":[],"data_is_untrusted":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/", "", "", time.Second, domain.MaxResponseSize)
	if err != nil {
		t.Fatal(err)
	}
	posted := true
	_, err = client.GetLatestDocuments(context.Background(), domain.LatestDocumentsRequest{Posted: &posted, DateFrom: "2026-01-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || got.Posted == nil || !*got.Posted || got.DateFrom == "" {
		t.Fatalf("request = %#v, method=%s", got, gotMethod)
	}
}

func TestClientReadsDocumentTablePart(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		_, _ = response.Write([]byte(`{"document_type":"Document.Test","uuid":"00000000-0000-0000-0000-000000000001","table_part":"Товары","columns":[],"rows":[],"returned_count":0,"limit":100,"truncated":false,"filters":[],"data_is_untrusted":true}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL+"/publication/hs/mcp-data/", "", "", time.Second, domain.MaxResponseSize)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.GetDocumentTablePart(context.Background(), domain.DocumentTablePartRequest{DocumentType: "Document.Test", UUID: "00000000-0000-0000-0000-000000000001", TablePart: "Товары", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/publication/hs/mcp-data/document-table-parts" || result.TablePart != "Товары" {
		t.Fatalf("path=%q result=%#v", gotPath, result)
	}
}
