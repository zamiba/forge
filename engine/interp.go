package engine

import (
	"fmt"
	"regexp"
	"strings"
)

// argRe matches an interpolated reference. Braces are required: a name may be
// namespaced with a dot, and without braces there is no way to tell where
// ${platform.slug}.zip ends and the literal text begins.
var argRe = regexp.MustCompile(`\$\{([\w.]+)\}`)

// bareArgRe matches the unbraced form that older specs used. Interpolate no
// longer substitutes it — UndeclaredArgs reports it instead, so a spec written
// against the old syntax fails a check rather than silently comparing the
// literal text "$platform" against something.
var bareArgRe = regexp.MustCompile(`\$(\w+)`)

// Interpolate replaces ${name} with values from args.
//
// Names are namespaced: ${args.region} is a user-configurable argument,
// ${platform.slug} and ${version.tag} are variables bound by the build's
// targetPlatforms and versions tables, and bare ${platform} and ${version} are
// the selected platform name and version string themselves.
//
// References to names not present in args are left untouched rather than
// blanked, so a typo surfaces as a visibly wrong path instead of a silently
// truncated one.
func Interpolate(s string, args map[string]string) string {
	if s == "" || !strings.ContainsRune(s, '$') {
		return s
	}
	return argRe.ReplaceAllStringFunc(s, func(match string) string {
		if v, ok := args[match[2:len(match)-1]]; ok {
			return v
		}
		return match
	})
}

// EvalCondition evaluates a step's "if" expression after interpolating args.
//
// Equality — "lhs == rhs" and "lhs != rhs" — compares the two sides as strings.
// A bare value is true when it is non-empty and not "false" or "0".
//
// Ordered comparison — ">", ">=", "<", "<=" — is resolved by *position in order*,
// not by parsing the operands as version numbers. order is the version hierarchy
// the spec file declares, oldest first, so "$version >= 1.1.0" asks whether the
// version being built appears at or after "1.1.0" in that list.
//
// Nothing here tries to understand a version string, because the strings in the
// wild do not support it: a catalog holding "Barnard Alfa", "1.1 RC4" and
// "Deckard Alfa (1.0.0)" has no parseable ordering, and a parser that guessed one
// would fail silently rather than loudly. An operand that is not a declared
// version is an error for the same reason — a mistyped threshold that quietly
// evaluated false would disable a build step with no sign anything went wrong.
func EvalCondition(expr string, args map[string]string, order []string) (bool, error) {
	expr = strings.TrimSpace(Interpolate(expr, args))

	// Two-character operators are tested first: ">=" also contains ">".
	for _, op := range []string{"!=", "==", ">=", "<=", ">", "<"} {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		lhs := strings.TrimSpace(expr[:idx])
		rhs := strings.TrimSpace(expr[idx+len(op):])

		switch op {
		case "==":
			return lhs == rhs, nil
		case "!=":
			return lhs != rhs, nil
		}

		li, ri := indexOf(order, lhs), indexOf(order, rhs)
		if li < 0 || ri < 0 {
			missing := lhs
			if li >= 0 {
				missing = rhs
			}
			return false, fmt.Errorf("condition %q: %q is not a declared version (declared: %s)",
				expr, missing, strings.Join(order, ", "))
		}
		switch op {
		case ">":
			return li > ri, nil
		case ">=":
			return li >= ri, nil
		case "<":
			return li < ri, nil
		case "<=":
			return li <= ri, nil
		}
	}

	return expr != "" && expr != "false" && expr != "0", nil
}

func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}
