package database

import (
	"path/filepath"
	"strings"
	"unicode"
)

type sqlTokenKind uint8

const (
	sqlTokenWord sqlTokenKind = iota
	sqlTokenString
	sqlTokenQuoted
	sqlTokenPunctuation
)

type sqlToken struct {
	kind  sqlTokenKind
	text  string
	value string
	start int
	end   int
}

type sqlPathEdit struct {
	start int
	end   int
	path  string
}

var sqlReaderFunctions = map[string]struct{}{
	"csv_scan":              {},
	"delta_scan":            {},
	"glob":                  {},
	"iceberg_scan":          {},
	"parquet_file_metadata": {},
	"parquet_metadata":      {},
	"parquet_schema":        {},
	"parquet_scan":          {},
	"read_blob":             {},
	"read_csv":              {},
	"read_csv_auto":         {},
	"read_json":             {},
	"read_json_auto":        {},
	"read_json_objects":     {},
	"read_ndjson":           {},
	"read_ndjson_auto":      {},
	"read_ndjson_objects":   {},
	"read_parquet":          {},
	"read_text":             {},
	"read_xls":              {},
	"read_xlsx":             {},
	"sniff_csv":             {},
}

// resolveSQLPaths makes relative file paths in SQL explicit before DuckDB
// executes them. Relative paths are based on baseDir instead of the process
// working directory. Script includes (.read) are expanded separately.
func resolveSQLPaths(query, baseDir string) string {
	if baseDir == "" || strings.TrimSpace(query) == "" {
		return query
	}

	tokens := tokenizeSQL(query)
	edits := make([]sqlPathEdit, 0)
	addPathEdit := func(index int) {
		token := tokens[index]
		resolved := resolveFilePath(token.value, baseDir)
		if resolved == token.value {
			return
		}
		edits = append(edits, sqlPathEdit{
			start: token.start,
			end:   token.end,
			path:  quoteSQLString(resolved),
		})
	}

	for index, token := range tokens {
		if token.kind != sqlTokenWord {
			continue
		}

		word := strings.ToLower(token.text)
		switch word {
		case "copy":
			for _, pathIndex := range copyPathIndexes(tokens, index) {
				addPathEdit(pathIndex)
			}
		case "attach":
			if pathIndex, ok := attachPathIndex(tokens, index); ok {
				addPathEdit(pathIndex)
			}
		case "export":
			if pathIndex, ok := exportDatabasePathIndex(tokens, index); ok {
				addPathEdit(pathIndex)
			}
		default:
			if _, ok := sqlReaderFunctions[word]; !ok {
				continue
			}
			for _, pathIndex := range readerPathIndexes(tokens, index) {
				addPathEdit(pathIndex)
			}
		}
	}

	if len(edits) == 0 {
		return query
	}

	var builder strings.Builder
	builder.Grow(len(query) + len(edits)*16)
	offset := 0
	for _, edit := range edits {
		if edit.start < offset {
			continue
		}
		builder.WriteString(query[offset:edit.start])
		builder.WriteString(edit.path)
		offset = edit.end
	}
	builder.WriteString(query[offset:])
	return builder.String()
}

func copyPathIndexes(tokens []sqlToken, copyIndex int) []int {
	indexes := make([]int, 0, 2)
	depth := 0
	for index := copyIndex + 1; index < len(tokens); index++ {
		token := tokens[index]
		switch token.text {
		case "(":
			depth++
			continue
		case ")":
			if depth > 0 {
				depth--
			}
			continue
		case ";":
			if depth == 0 {
				return indexes
			}
			continue
		}
		if depth != 0 || token.kind != sqlTokenWord {
			continue
		}
		word := strings.ToLower(token.text)
		if word != "from" && word != "to" {
			continue
		}
		if index+1 >= len(tokens) || tokens[index+1].kind != sqlTokenString {
			continue
		}
		indexes = append(indexes, index+1)
	}
	return indexes
}

func attachPathIndex(tokens []sqlToken, attachIndex int) (int, bool) {
	index := attachIndex + 1
	if index+3 < len(tokens) &&
		sqlWordEqual(tokens[index], "if") &&
		sqlWordEqual(tokens[index+1], "not") &&
		sqlWordEqual(tokens[index+2], "exists") {
		index += 3
	}
	if index >= len(tokens) || tokens[index].kind != sqlTokenString {
		return 0, false
	}
	return index, true
}

func exportDatabasePathIndex(tokens []sqlToken, exportIndex int) (int, bool) {
	index := exportIndex + 1
	if index >= len(tokens) || !sqlWordEqual(tokens[index], "database") {
		return 0, false
	}
	index++
	if index >= len(tokens) || tokens[index].kind != sqlTokenString {
		return 0, false
	}
	return index, true
}

func readerPathIndexes(tokens []sqlToken, functionIndex int) []int {
	index := functionIndex + 1
	if index >= len(tokens) || tokens[index].text != "(" {
		return nil
	}
	index++
	if index >= len(tokens) {
		return nil
	}

	if tokens[index].kind == sqlTokenString {
		return []int{index}
	}
	if tokens[index].text != "[" {
		return nil
	}

	index++
	indexes := make([]int, 0, 2)
	expectString := true
	for ; index < len(tokens); index++ {
		token := tokens[index]
		if token.text == "]" {
			return indexes
		}
		if expectString {
			if token.kind != sqlTokenString {
				return nil
			}
			indexes = append(indexes, index)
			expectString = false
			continue
		}
		if token.text != "," {
			return nil
		}
		expectString = true
	}
	return nil
}

func sqlWordEqual(token sqlToken, value string) bool {
	return token.kind == sqlTokenWord && strings.EqualFold(token.text, value)
}

func resolveFilePath(value, baseDir string) string {
	if value == "" ||
		filepath.IsAbs(value) ||
		strings.HasPrefix(value, "\\\\") ||
		hasURIScheme(value) {
		return value
	}
	path := filepath.FromSlash(value)
	return filepath.Clean(filepath.Join(baseDir, path))
}

func hasURIScheme(value string) bool {
	schemeEnd := strings.Index(value, "://")
	if schemeEnd <= 0 {
		return false
	}
	for index, character := range value[:schemeEnd] {
		if index == 0 {
			if !unicode.IsLetter(character) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(character) &&
			!unicode.IsDigit(character) &&
			character != '+' &&
			character != '-' &&
			character != '.' {
			return false
		}
	}
	return true
}

func quoteSQLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func tokenizeSQL(query string) []sqlToken {
	tokens := make([]sqlToken, 0, 64)
	for index := 0; index < len(query); {
		character := query[index]
		switch {
		case isSQLSpace(character):
			index++
		case character == '-' && index+1 < len(query) && query[index+1] == '-':
			index = skipSQLLineComment(query, index+2)
		case character == '/' && index+1 < len(query) && query[index+1] == '*':
			index = skipSQLBlockComment(query, index+2)
		case character == '\'':
			start := index
			index++
			var value strings.Builder
			for index < len(query) {
				if query[index] != '\'' {
					value.WriteByte(query[index])
					index++
					continue
				}
				if index+1 < len(query) && query[index+1] == '\'' {
					value.WriteByte('\'')
					index += 2
					continue
				}
				index++
				break
			}
			tokens = append(tokens, sqlToken{
				kind:  sqlTokenString,
				value: value.String(),
				start: start,
				end:   index,
			})
		case character == '"':
			start := index
			index++
			for index < len(query) {
				if query[index] != '"' {
					index++
					continue
				}
				if index+1 < len(query) && query[index+1] == '"' {
					index += 2
					continue
				}
				index++
				break
			}
			tokens = append(tokens, sqlToken{
				kind:  sqlTokenQuoted,
				text:  query[start:index],
				start: start,
				end:   index,
			})
		case character == '$' && index+1 < len(query) && query[index+1] == '$':
			start := index
			index += 2
			if endOffset := strings.Index(query[index:], "$$"); endOffset >= 0 {
				index += endOffset + 2
			} else {
				index = len(query)
			}
			tokens = append(tokens, sqlToken{
				kind:  sqlTokenQuoted,
				text:  query[start:index],
				start: start,
				end:   index,
			})
		case isSQLWordStart(character):
			start := index
			index++
			for index < len(query) && isSQLWordPart(query[index]) {
				index++
			}
			tokens = append(tokens, sqlToken{
				kind:  sqlTokenWord,
				text:  query[start:index],
				start: start,
				end:   index,
			})
		default:
			start := index
			index++
			for index < len(query) && query[index] >= 0x80 && query[index] <= 0xff {
				index++
			}
			tokens = append(tokens, sqlToken{
				kind:  sqlTokenPunctuation,
				text:  query[start:index],
				start: start,
				end:   index,
			})
		}
	}
	return tokens
}

func skipSQLLineComment(query string, index int) int {
	for index < len(query) && query[index] != '\n' {
		index++
	}
	return index
}

func skipSQLBlockComment(query string, index int) int {
	depth := 1
	for index < len(query) && depth > 0 {
		switch {
		case index+1 < len(query) && query[index] == '/' && query[index+1] == '*':
			depth++
			index += 2
		case index+1 < len(query) && query[index] == '*' && query[index+1] == '/':
			depth--
			index += 2
		default:
			index++
		}
	}
	return index
}

func isSQLSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func isSQLWordStart(character byte) bool {
	return character == '_' ||
		character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z'
}

func isSQLWordPart(character byte) bool {
	return isSQLWordStart(character) ||
		character >= '0' && character <= '9' ||
		character == '$'
}
