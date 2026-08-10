package engine

import (
	"regexp"
	"strings"
)

var argRe = regexp.MustCompile(`\$\{(\w+)\}|\$(\w+)`)

// Interpolate replaces $name and ${name} with values from args. References to
// names not present in args are left untouched rather than blanked, so a typo
// surfaces as a visibly wrong path instead of a silently truncated one.
func Interpolate(s string, args map[string]string) string {
	if s == "" || !strings.ContainsRune(s, '$') {
		return s
	}
	return argRe.ReplaceAllStringFunc(s, func(match string) string {
		var key string
		if strings.HasPrefix(match, "${") {
			key = match[2 : len(match)-1]
		} else {
			key = match[1:]
		}
		if v, ok := args[key]; ok {
			return v
		}
		return match
	})
}

// EvalCondition evaluates a step's "if" expression after interpolating args.
// Supports "lhs != rhs" and "lhs == rhs"; a bare value is true when it is
// non-empty and not "false" or "0".
func EvalCondition(expr string, args map[string]string) bool {
	expr = strings.TrimSpace(Interpolate(expr, args))
	if idx := strings.Index(expr, "!="); idx >= 0 {
		return strings.TrimSpace(expr[:idx]) != strings.TrimSpace(expr[idx+2:])
	}
	if idx := strings.Index(expr, "=="); idx >= 0 {
		return strings.TrimSpace(expr[:idx]) == strings.TrimSpace(expr[idx+2:])
	}
	return expr != "" && expr != "false" && expr != "0"
}
