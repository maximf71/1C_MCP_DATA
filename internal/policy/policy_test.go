package policy

import (
	"strings"
	"testing"

	"github.com/codex/mcp-1c-data/internal/domain"
)

func TestValidateReadOnlyQuery(t *testing.T) {
	result, err := Validate(
		"ВЫБРАТЬ ПЕРВЫЕ 20 Т.Код, Т.Наименование ИЗ Справочник.Номенклатура КАК Т ГДЕ Т.Наименование ПОДОБНО &Поиск",
		map[string]domain.TypedValue{"Поиск": {Type: "string", Value: "%болт%"}},
		20,
	)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(result.Sources) != 1 || result.Sources[0] != "СПРАВОЧНИК.НОМЕНКЛАТУРА" {
		t.Fatalf("sources = %#v", result.Sources)
	}
}

func TestValidateRejectsDangerousQueries(t *testing.T) {
	tests := map[string]string{
		"batch":     "ВЫБРАТЬ ПЕРВЫЕ 1 Т.Код ИЗ Справочник.Товары КАК Т; ВЫБРАТЬ 1",
		"temp":      "ВЫБРАТЬ Т.Код ПОМЕСТИТЬ ВТ ИЗ Справочник.Товары КАК Т",
		"star":      "ВЫБРАТЬ ПЕРВЫЕ 10 * ИЗ Справочник.Товары",
		"unbounded": "ВЫБРАТЬ Т.Код ИЗ Справочник.Товары КАК Т",
		"comment":   "ВЫБРАТЬ ПЕРВЫЕ 1 Т.Код ИЗ Справочник.Товары КАК Т // x",
		"mutation":  "УДАЛИТЬ ИЗ Справочник.Товары",
		"deep":      "ВЫБРАТЬ СУММА(((((Т.Сумма))))) ИЗ Документ.Продажа КАК Т",
		"register":  "ВЫБРАТЬ СУММА(Т.СуммаОборот) ИЗ РегистрНакопления.Продажи.Обороты КАК Т",
		"literal":   "ВЫБРАТЬ ПЕРВЫЕ 10 Т.Код ИЗ Справочник.Товары КАК Т ГДЕ Т.Код = \"001\"",
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Validate(query, nil, 10); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestValidateRejectsOversizedQuery(t *testing.T) {
	query := "ВЫБРАТЬ ПЕРВЫЕ 1 Т.Код ИЗ Справочник.Товары КАК Т " + strings.Repeat(" ", domain.MaxQueryBytes)
	if _, err := Validate(query, nil, 1); err == nil {
		t.Fatal("expected oversized query rejection")
	}
}
