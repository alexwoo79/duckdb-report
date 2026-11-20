package template

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var variablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

var ErrVariableMissing = errors.New("template variable is missing")

func Resolve(input string, params map[string]string) (string, error) {
	var missing []string
	result := variablePattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := variablePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		value, ok := params[parts[1]]
		if !ok {
			missing = append(missing, parts[1])
			return match
		}
		return value
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("%w: %s", ErrVariableMissing, strings.Join(missing, ", "))
	}
	return result, nil
}
