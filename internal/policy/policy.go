package policy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codex/mcp-1c-data/internal/domain"
)

var (
	startPattern         = regexp.MustCompile(`(?is)^\s*(?:ВЫБРАТЬ|SELECT)(?:\s|$)`)
	forbiddenPattern     = regexp.MustCompile(`(?i)(?:^|[^A-ZА-ЯЁ0-9_])(?:ПОМЕСТИТЬ|INTO|УНИЧТОЖИТЬ|DROP|UPDATE|DELETE|INSERT|ИЗМЕНИТЬ|УДАЛИТЬ|ДОБАВИТЬ|ВЫПОЛНИТЬ)(?:$|[^A-ZА-ЯЁ0-9_])`)
	starPattern          = regexp.MustCompile(`(?i)(ВЫБРАТЬ|SELECT)(?:\s+РАЗРЕШЕННЫЕ)?(?:\s+(?:ПЕРВЫЕ|TOP)\s+\d+)?\s+(?:[A-ZА-ЯЁ_][A-ZА-ЯЁ0-9_]*\.)?\*`)
	joinPattern          = regexp.MustCompile(`(?i)(?:^|[^A-ZА-ЯЁ0-9_])(?:СОЕДИНЕНИЕ|JOIN)(?:$|[^A-ZА-ЯЁ0-9_])`)
	sourcePattern        = regexp.MustCompile(`(?i)(?:^|[^A-ZА-ЯЁ0-9_])(?:ИЗ|FROM|СОЕДИНЕНИЕ|JOIN)\s+([A-ZА-ЯЁ_][A-ZА-ЯЁ0-9_]*(?:\.[A-ZА-ЯЁ_][A-ZА-ЯЁ0-9_]*){1,3})`)
	topPattern           = regexp.MustCompile(`(?i)(?:^|[^A-ZА-ЯЁ0-9_])(?:ПЕРВЫЕ|TOP)\s+(\d+)(?:$|[^A-ZА-ЯЁ0-9_])`)
	aggregatePattern     = regexp.MustCompile(`(?i)(?:^|[^A-ZА-ЯЁ0-9_])(?:КОЛИЧЕСТВО|СУММА|МИНИМУМ|МАКСИМУМ|СРЕДНЕЕ|COUNT|SUM|MIN|MAX|AVG)\s*\(`)
	parameterPattern     = regexp.MustCompile(`&([A-ZА-ЯЁ_][A-ZА-ЯЁ0-9_]*)`)
	commentPattern       = regexp.MustCompile(`(?s)//|/\*|\*/|--`)
	virtualPeriodPattern = regexp.MustCompile(`(?i)\.(?:ОСТАТКИ|ОБОРОТЫ|ОСТАТКИИОБОРОТЫ|СРЕЗПОСЛЕДНИХ|СРЕЗПЕРВЫХ)\s*\(\s*&`)
)

type Result struct {
	Sources  []string
	Warnings []string
	Filters  []string
}

func Validate(query string, parameters map[string]domain.TypedValue, limit int) (Result, error) {
	var result Result
	if strings.TrimSpace(query) == "" {
		return result, fmt.Errorf("QUERY_EMPTY: query is required")
	}
	if len([]byte(query)) > domain.MaxQueryBytes {
		return result, fmt.Errorf("QUERY_TOO_LARGE: query exceeds %d bytes", domain.MaxQueryBytes)
	}
	if !utf8.ValidString(query) {
		return result, fmt.Errorf("QUERY_ENCODING: query must be valid UTF-8")
	}
	if !startPattern.MatchString(query) {
		return result, fmt.Errorf("QUERY_NOT_READ_ONLY: only SELECT/ВЫБРАТЬ is allowed")
	}
	if strings.ContainsRune(query, ';') {
		return result, fmt.Errorf("QUERY_BATCH_FORBIDDEN: semicolons and query batches are not allowed")
	}
	if commentPattern.MatchString(query) {
		return result, fmt.Errorf("QUERY_COMMENTS_FORBIDDEN: comments are not allowed")
	}
	if strings.ContainsRune(query, '"') {
		return result, fmt.Errorf("QUERY_LITERAL_FORBIDDEN: string values must be supplied as typed parameters")
	}
	if forbiddenPattern.MatchString(query) {
		return result, fmt.Errorf("QUERY_NOT_READ_ONLY: forbidden keyword detected")
	}
	if starPattern.MatchString(query) {
		return result, fmt.Errorf("QUERY_STAR_FORBIDDEN: select explicit fields instead of *")
	}
	if limit < 1 || limit > domain.MaxRows {
		return result, fmt.Errorf("LIMIT_INVALID: limit must be between 1 and %d", domain.MaxRows)
	}
	if joins := len(joinPattern.FindAllStringIndex(query, -1)); joins > 5 {
		return result, fmt.Errorf("QUERY_TOO_COMPLEX: at most 5 joins are allowed")
	}
	if depth, balanced := parenthesisDepth(query); !balanced {
		return result, fmt.Errorf("QUERY_SYNTAX: unbalanced parentheses")
	} else if depth > 4 {
		return result, fmt.Errorf("QUERY_TOO_COMPLEX: parenthesis nesting must not exceed 4")
	}
	for _, match := range sourcePattern.FindAllStringSubmatch(strings.ToUpper(query), -1) {
		result.Sources = appendUnique(result.Sources, match[1])
	}
	if len(result.Sources) == 0 {
		return result, fmt.Errorf("QUERY_SOURCE_MISSING: no 1C query source was found")
	}
	if len(result.Sources) > 10 {
		return result, fmt.Errorf("QUERY_TOO_COMPLEX: at most 10 sources are allowed")
	}
	if !aggregatePattern.MatchString(query) {
		match := topPattern.FindStringSubmatch(query)
		if match == nil {
			return result, fmt.Errorf("QUERY_UNBOUNDED: a non-aggregate query must use ПЕРВЫЕ/TOP")
		}
		top, _ := strconv.Atoi(match[1])
		if top < 1 || top > limit {
			return result, fmt.Errorf("QUERY_TOP_INVALID: ПЕРВЫЕ/TOP must be between 1 and the requested limit")
		}
	}
	normalizedParameters := make(map[string]struct{}, len(parameters))
	for name, value := range parameters {
		upperName := strings.ToUpper(strings.TrimSpace(name))
		if !validIdentifier(name) {
			return result, fmt.Errorf("PARAMETER_NAME_INVALID: %q is not a valid parameter name", name)
		}
		if err := validateTypedValue(value); err != nil {
			return result, fmt.Errorf("PARAMETER_INVALID: %s: %w", name, err)
		}
		normalizedParameters[upperName] = struct{}{}
	}
	for _, match := range parameterPattern.FindAllStringSubmatch(strings.ToUpper(query), -1) {
		if _, ok := normalizedParameters[match[1]]; !ok {
			return result, fmt.Errorf("PARAMETER_MISSING: parameter %s is not supplied", match[1])
		}
	}
	for name := range normalizedParameters {
		if !strings.Contains(strings.ToUpper(query), "&"+name) {
			return result, fmt.Errorf("PARAMETER_UNUSED: parameter %s is not used by the query", name)
		}
	}
	result.Filters = extractFilterHints(query)
	registerSource := false
	for _, source := range result.Sources {
		if strings.Contains(source, "РЕГИСТР") || strings.Contains(source, "REGISTER") {
			registerSource = true
			break
		}
	}
	if registerSource && len(result.Filters) == 0 && !virtualPeriodPattern.MatchString(query) {
		return result, fmt.Errorf("QUERY_FILTER_REQUIRED: register queries require a WHERE/ГДЕ filter or a parameterized virtual-table period")
	}
	if len(result.Filters) == 0 {
		result.Warnings = append(result.Warnings, "No explicit WHERE/ГДЕ filter was detected.")
	}
	return result, nil
}

func validateTypedValue(value domain.TypedValue) error {
	switch strings.ToLower(strings.TrimSpace(value.Type)) {
	case "string":
		if _, ok := value.Value.(string); !ok {
			return fmt.Errorf("string value expected")
		}
	case "number":
		switch value.Value.(type) {
		case float64, float32, int, int32, int64, jsonNumber:
		default:
			return fmt.Errorf("numeric value expected")
		}
	case "boolean":
		if _, ok := value.Value.(bool); !ok {
			return fmt.Errorf("boolean value expected")
		}
	case "date", "datetime":
		if _, ok := value.Value.(string); !ok {
			return fmt.Errorf("ISO-8601 string expected")
		}
	case "null":
		if value.Value != nil {
			return fmt.Errorf("null must not have a value")
		}
	case "reference":
		if value.Metadata == "" || value.UUID == "" {
			return fmt.Errorf("reference requires metadata and uuid")
		}
	case "enum":
		if value.Metadata == "" {
			return fmt.Errorf("enum requires metadata")
		}
		if _, ok := value.Value.(string); !ok {
			return fmt.Errorf("enum value name must be a string")
		}
	default:
		return fmt.Errorf("unsupported type %q", value.Type)
	}
	return nil
}

// jsonNumber permits tests and direct callers to use a named JSON-number-like
// string without weakening validation for arbitrary strings.
type jsonNumber string

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if index == 0 && unicode.IsDigit(r) {
			return false
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func appendUnique(values []string, candidate string) []string {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return values
		}
	}
	return append(values, candidate)
}

func extractFilterHints(query string) []string {
	upper := strings.ToUpper(query)
	for _, keyword := range []string{" ГДЕ ", "\nГДЕ ", " WHERE ", "\nWHERE "} {
		if index := strings.Index(upper, keyword); index >= 0 {
			value := strings.TrimSpace(query[index+len(keyword):])
			for _, end := range []string{" СГРУППИРОВАТЬ ", " GROUP BY ", " УПОРЯДОЧИТЬ ", " ORDER BY "} {
				if endIndex := strings.Index(strings.ToUpper(value), end); endIndex >= 0 {
					value = strings.TrimSpace(value[:endIndex])
				}
			}
			if len([]rune(value)) > 300 {
				value = string([]rune(value)[:300]) + "…"
			}
			if value != "" {
				return []string{value}
			}
		}
	}
	return nil
}

func parenthesisDepth(query string) (int, bool) {
	current, maximum := 0, 0
	inString := false
	runes := []rune(query)
	for index := 0; index < len(runes); index++ {
		switch runes[index] {
		case '"':
			if inString && index+1 < len(runes) && runes[index+1] == '"' {
				index++
				continue
			}
			inString = !inString
		case '(':
			if !inString {
				current++
				if current > maximum {
					maximum = current
				}
			}
		case ')':
			if !inString {
				current--
				if current < 0 {
					return maximum, false
				}
			}
		}
	}
	return maximum, !inString && current == 0
}
