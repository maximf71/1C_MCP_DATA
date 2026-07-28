package metadata

import (
	"testing"

	"github.com/codex/mcp-1c-data/internal/domain"
)

func TestSearchFiltersByExactFieldBeforePaging(t *testing.T) {
	catalog := domain.MetadataCatalog{Objects: []domain.MetadataObject{
		{Type: "Document", Name: "АвансовыйОтчет", Synonym: "Авансовый отчет", QuerySource: "Документ.АвансовыйОтчет", CanRead: true, Fields: []string{"Организация", "Ответственный"}},
		{Type: "Document", Name: "Поступление", Synonym: "Поступление", QuerySource: "Документ.Поступление", CanRead: true, Fields: []string{"Организация"}},
		{Type: "Catalog", Name: "Пользователи", QuerySource: "Справочник.Пользователи", CanRead: true, Fields: []string{"Наименование"}},
	}}
	result := Search(catalog, "", []string{"Document"}, "ответственный", 0)
	if len(result) != 1 || result[0].Name != "АвансовыйОтчет" {
		t.Fatalf("result = %#v", result)
	}
}
