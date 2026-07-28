package domain

import "encoding/json"

const (
	MaxRows         = 200
	MaxQueryBytes   = 32 * 1024
	MaxResponseSize = 4 * 1024 * 1024
)

type TypedValue struct {
	Type     string `json:"type"`
	Value    any    `json:"value,omitempty"`
	Metadata string `json:"metadata,omitempty"`
	UUID     string `json:"uuid,omitempty"`
}

type MetadataObject struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Synonym     string   `json:"synonym,omitempty"`
	QuerySource string   `json:"query_source"`
	CanRead     bool     `json:"can_read"`
	Fields      []string `json:"fields,omitempty"`
}

type MetadataCatalog struct {
	Configuration string           `json:"configuration,omitempty"`
	Version       string           `json:"version,omitempty"`
	Objects       []MetadataObject `json:"objects"`
}

type SchemaRequest struct {
	Objects []ObjectRef `json:"objects"`
}

type ObjectRef struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type SchemaResponse struct {
	Objects []ObjectSchema `json:"objects"`
}

type ObjectSchema struct {
	Type          string         `json:"type"`
	Name          string         `json:"name"`
	Synonym       string         `json:"synonym,omitempty"`
	QuerySource   string         `json:"query_source"`
	Fields        []Field        `json:"fields"`
	TableParts    []TablePart    `json:"table_parts,omitempty"`
	VirtualTables []VirtualTable `json:"virtual_tables,omitempty"`
}

type Field struct {
	Name    string `json:"name"`
	Synonym string `json:"synonym,omitempty"`
	Type    string `json:"type"`
}

type TablePart struct {
	Name        string  `json:"name"`
	Synonym     string  `json:"synonym,omitempty"`
	QuerySource string  `json:"query_source"`
	Fields      []Field `json:"fields"`
}

type VirtualTable struct {
	Name        string  `json:"name"`
	QuerySource string  `json:"query_source"`
	Fields      []Field `json:"fields,omitempty"`
}

type ValidateRequest struct {
	Query      string                `json:"query"`
	Parameters map[string]TypedValue `json:"parameters,omitempty"`
	Limit      int                   `json:"limit"`
}

type ValidateResponse struct {
	Valid       bool     `json:"valid"`
	Columns     []string `json:"columns,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	ErrorCode   string   `json:"error_code,omitempty"`
	Error       string   `json:"error,omitempty"`
	Fingerprint string   `json:"schema_fingerprint,omitempty"`
}

type ReferenceFilter struct {
	Metadata string `json:"metadata"`
	UUID     string `json:"uuid"`
}

type LatestDocumentsRequest struct {
	Responsible  *ReferenceFilter `json:"responsible,omitempty"`
	Posted       *bool            `json:"posted,omitempty"`
	Organization *ReferenceFilter `json:"organization,omitempty"`
	DateFrom     string           `json:"date_from,omitempty"`
	DateTo       string           `json:"date_to,omitempty"`
}

type ExecuteRequest struct {
	Query      string                `json:"query"`
	Parameters map[string]TypedValue `json:"parameters,omitempty"`
	Limit      int                   `json:"limit"`
}

type QueryResult struct {
	Columns       []ResultColumn   `json:"columns"`
	Rows          []map[string]any `json:"rows"`
	ReturnedCount int              `json:"returned_count"`
	Limit         int              `json:"limit"`
	Truncated     bool             `json:"truncated"`
	ExecutionMS   int64            `json:"execution_ms,omitempty"`
	Filters       []string         `json:"filters,omitempty"`
	DataUntrusted bool             `json:"data_is_untrusted"`
}

type ResultColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type LatestDocument struct {
	Metadata     string `json:"metadata"`
	Synonym      string `json:"synonym,omitempty"`
	Presentation string `json:"presentation"`
	UUID         string `json:"uuid"`
	Number       string `json:"number"`
	Date         string `json:"date"`
	Posted       bool   `json:"posted"`
	DeletionMark bool   `json:"deletion_mark"`
}

type LatestDocumentsResult struct {
	Documents             []LatestDocument `json:"documents"`
	MaxDate               string           `json:"max_date,omitempty"`
	ReturnedCount         int              `json:"returned_count"`
	Truncated             bool             `json:"truncated"`
	ReadableDocumentTypes int              `json:"readable_document_types"`
	ScannedDocumentTypes  int              `json:"scanned_document_types"`
	Complete              bool             `json:"complete"`
	Filters               []string         `json:"filters"`
	ExecutionMS           int64            `json:"execution_ms,omitempty"`
	DataUntrusted         bool             `json:"data_is_untrusted"`
}

type DocumentTablePartRequest struct {
	DocumentType string `json:"document_type"`
	UUID         string `json:"uuid"`
	TablePart    string `json:"table_part"`
	Limit        int    `json:"limit"`
}

type DocumentTablePartResult struct {
	DocumentType  string           `json:"document_type"`
	UUID          string           `json:"uuid"`
	TablePart     string           `json:"table_part"`
	Columns       []ResultColumn   `json:"columns"`
	Rows          []map[string]any `json:"rows"`
	ReturnedCount int              `json:"returned_count"`
	Limit         int              `json:"limit"`
	Truncated     bool             `json:"truncated"`
	Filters       []string         `json:"filters"`
	ExecutionMS   int64            `json:"execution_ms,omitempty"`
	DataUntrusted bool             `json:"data_is_untrusted"`
}

type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Error RemoteError `json:"error"`
}

func CloneParameters(in map[string]TypedValue) map[string]TypedValue {
	out := make(map[string]TypedValue, len(in))
	for key, value := range in {
		// Values originate in decoded JSON. A marshal roundtrip prevents a tool
		// caller from mutating a plan after it has been validated.
		raw, _ := json.Marshal(value.Value)
		var cloned any
		_ = json.Unmarshal(raw, &cloned)
		value.Value = cloned
		out[key] = value
	}
	return out
}
