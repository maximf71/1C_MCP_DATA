package server

import (
	"context"
	"testing"

	"github.com/codex/mcp-1c-data/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeRemote struct {
	executions    int
	latest        domain.LatestDocumentsResult
	latestRequest domain.LatestDocumentsRequest
	validation    *domain.ValidateResponse
}

func (f *fakeRemote) GetConfigurationInfo(context.Context) (map[string]any, error) {
	return map[string]any{"configuration": "Synthetic", "user": "Reader"}, nil
}

func (f *fakeRemote) GetMetadataCatalog(context.Context) (domain.MetadataCatalog, error) {
	return domain.MetadataCatalog{
		Configuration: "Synthetic",
		Objects: []domain.MetadataObject{
			{Type: "Catalog", Name: "Номенклатура", Synonym: "Номенклатура", QuerySource: "Справочник.Номенклатура", CanRead: true},
			{Type: "Document", Name: "АвансовыйОтчет", Synonym: "Авансовый отчет", QuerySource: "Документ.АвансовыйОтчет", CanRead: true, Fields: []string{"Ответственный", "Организация"}},
			{Type: "Document", Name: "Заказ", Synonym: "Заказ", QuerySource: "Документ.Заказ", CanRead: true, Fields: []string{"Ответственный"}},
		},
	}, nil
}

func (f *fakeRemote) GetQuerySchema(_ context.Context, request domain.SchemaRequest) (domain.SchemaResponse, error) {
	if len(request.Objects) == 1 && request.Objects[0].Type == "Document" {
		return domain.SchemaResponse{Objects: []domain.ObjectSchema{{
			Type: "Document", Name: request.Objects[0].Name, QuerySource: "Документ." + request.Objects[0].Name,
			TableParts: []domain.TablePart{{Name: "Товары", QuerySource: "Документ." + request.Objects[0].Name + ".Товары"}},
		}}}, nil
	}
	return domain.SchemaResponse{Objects: []domain.ObjectSchema{{
		Type: "Catalog", Name: "Номенклатура", QuerySource: "Справочник.Номенклатура",
		Fields: []domain.Field{{Name: "Код", Type: "String"}},
	}}}, nil
}

func (f *fakeRemote) ValidateQuery(context.Context, domain.ValidateRequest) (domain.ValidateResponse, error) {
	if f.validation != nil {
		return *f.validation, nil
	}
	return domain.ValidateResponse{Valid: true, Columns: []string{"Код"}}, nil
}

func (f *fakeRemote) ExecuteQuery(context.Context, domain.ExecuteRequest) (domain.QueryResult, error) {
	f.executions++
	return domain.QueryResult{
		Columns:       []domain.ResultColumn{{Name: "Код", Type: "String"}},
		Rows:          []map[string]any{{"Код": "001"}},
		ReturnedCount: 1,
		Limit:         10,
	}, nil
}

func (f *fakeRemote) GetLatestDocuments(_ context.Context, request domain.LatestDocumentsRequest) (domain.LatestDocumentsResult, error) {
	f.latestRequest = request
	if f.latest.Filters == nil {
		f.latest = domain.LatestDocumentsResult{
			Documents: []domain.LatestDocument{{
				Metadata: "Document.ОперацияБух", Presentation: "Операция 1", UUID: "00000000-0000-0000-0000-000000000001",
				Number: "1", Date: "2026-07-26T12:00:00", Posted: false,
			}},
			MaxDate: "2026-07-26T12:00:00", ReturnedCount: 1,
			ReadableDocumentTypes: 2, ScannedDocumentTypes: 2, Complete: true,
		}
	}
	return f.latest, nil
}

func (f *fakeRemote) GetDocumentTablePart(context.Context, domain.DocumentTablePartRequest) (domain.DocumentTablePartResult, error) {
	return domain.DocumentTablePartResult{Columns: []domain.ResultColumn{{Name: "НомерСтроки", Type: "Number"}}, Rows: []map[string]any{{"НомерСтроки": float64(1)}}, ReturnedCount: 1}, nil
}

func TestMCPContractAndSingleUseExecution(t *testing.T) {
	ctx := context.Background()
	remote := &fakeRemote{}
	server := New(remote)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	list, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"get_configuration_info":   false,
		"search_metadata":          false,
		"get_query_schema":         false,
		"validate_query":           false,
		"execute_query":            false,
		"get_latest_documents":     false,
		"get_document_table_parts": false,
	}
	if len(list.Tools) != len(want) {
		t.Fatalf("tool count = %d", len(list.Tools))
	}
	for _, tool := range list.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		want[tool.Name] = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %s does not declare read-only", tool.Name)
		}
		if tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Fatalf("tool %s does not declare its 1C interaction", tool.Name)
		}
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Fatalf("tool %s is marked destructive", tool.Name)
		}
		if tool.Name == "execute_query" && tool.Annotations.IdempotentHint {
			t.Fatal("single-use execute_query must not be marked idempotent")
		}
		if tool.Name == "get_latest_documents" && !tool.Annotations.IdempotentHint {
			t.Fatal("get_latest_documents must be marked idempotent")
		}
		if tool.Name == "get_document_table_parts" && !tool.Annotations.IdempotentHint {
			t.Fatal("get_document_table_parts must be marked idempotent")
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing tool %q", name)
		}
	}

	validated, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "validate_query",
		Arguments: map[string]any{
			"query": "ВЫБРАТЬ ПЕРВЫЕ 10 Н.Код ИЗ Справочник.Номенклатура КАК Н",
			"limit": 10,
		},
	})
	if err != nil || validated.IsError {
		t.Fatalf("validate_query: result=%#v err=%v", validated, err)
	}
	structured, ok := validated.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured validation output = %T", validated.StructuredContent)
	}
	validationID, _ := structured["validation_id"].(string)
	if len(validationID) != 48 {
		t.Fatalf("validation_id = %q", validationID)
	}

	executed, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "execute_query", Arguments: map[string]any{"validation_id": validationID},
	})
	if err != nil || executed.IsError {
		t.Fatalf("execute_query: result=%#v err=%v", executed, err)
	}
	if remote.executions != 1 {
		t.Fatalf("executions = %d", remote.executions)
	}
	repeated, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "execute_query", Arguments: map[string]any{"validation_id": validationID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !repeated.IsError || remote.executions != 1 {
		t.Fatalf("repeated execute = %#v, executions=%d", repeated, remote.executions)
	}

	firstPage, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "search_metadata", Arguments: map[string]any{"types": []any{"Document"}, "field": "Ответственный", "limit": float64(1)}})
	if err != nil || firstPage.IsError {
		t.Fatalf("first metadata page: %#v, %v", firstPage, err)
	}
	firstStructured := firstPage.StructuredContent.(map[string]any)
	if firstStructured["has_more"] != true || firstStructured["next_cursor"] == "" {
		t.Fatalf("first metadata page = %#v", firstStructured)
	}
	secondPage, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "search_metadata", Arguments: map[string]any{"types": []any{"Document"}, "field": "Ответственный", "limit": float64(1), "cursor": firstStructured["next_cursor"]}})
	if err != nil || secondPage.IsError {
		t.Fatalf("second metadata page: %#v, %v", secondPage, err)
	}
	if secondPage.StructuredContent.(map[string]any)["has_more"] != false {
		t.Fatalf("second metadata page = %#v", secondPage.StructuredContent)
	}

	tablePart, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_document_table_parts", Arguments: map[string]any{
		"document_type": "Document.АвансовыйОтчет", "uuid": "00000000-0000-0000-0000-000000000001", "table_part": "Товары", "limit": float64(10),
	}})
	if err != nil || tablePart.IsError {
		t.Fatalf("table part: %#v, %v", tablePart, err)
	}
	if tablePart.StructuredContent.(map[string]any)["returned_count"] != float64(1) {
		t.Fatalf("table part = %#v", tablePart.StructuredContent)
	}
}

func TestCompileErrorIncludesSanitizedOneCDetail(t *testing.T) {
	remote := &fakeRemote{validation: &domain.ValidateResponse{Valid: false, Error: "{(1, 12)}: Поле не найдено\nсекция"}}
	server := New(remote)
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "validate_query", Arguments: map[string]any{
		"query": "ВЫБРАТЬ ПЕРВЫЕ 10 Н.Код ИЗ Справочник.Номенклатура КАК Н", "limit": float64(10),
	}})
	if err != nil || result.IsError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	message, _ := result.StructuredContent.(map[string]any)["error"].(string)
	if message != "{(1, 12)}: Поле не найдено секция" {
		t.Fatalf("compile error = %q", message)
	}
}

func TestLatestDocumentsFailsClosedAndRateLimits(t *testing.T) {
	ctx := context.Background()
	remote := &fakeRemote{}
	server := New(remote)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	for index := 0; index < 2; index++ {
		arguments := map[string]any{}
		if index == 1 {
			arguments = map[string]any{"posted": false, "date_from": "2026-01-01T00:00:00Z"}
		}
		result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_latest_documents", Arguments: arguments})
		if err != nil || result.IsError {
			t.Fatalf("latest call %d: result=%#v err=%v", index, result, err)
		}
	}
	if remote.latestRequest.Posted == nil || *remote.latestRequest.Posted || remote.latestRequest.DateFrom == "" {
		t.Fatalf("latest request = %#v", remote.latestRequest)
	}
	limited, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_latest_documents", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !limited.IsError {
		t.Fatal("third latest scan must be rate limited")
	}

	incompleteRemote := &fakeRemote{latest: domain.LatestDocumentsResult{
		ReadableDocumentTypes: 2, ScannedDocumentTypes: 1, Complete: false, Filters: []string{},
	}}
	incompleteServer := New(incompleteRemote)
	incompleteClient := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	sTransport, cTransport := mcp.NewInMemoryTransports()
	sSession, err := incompleteServer.Connect(ctx, sTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sSession.Close() })
	cSession, err := incompleteClient.Connect(ctx, cTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cSession.Close() })
	incomplete, err := cSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_latest_documents", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !incomplete.IsError {
		t.Fatal("incomplete coverage must fail closed")
	}
}
