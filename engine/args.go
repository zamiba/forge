package engine

import (
	"encoding/json"
	"sort"
)

// reservedArgs are injected by Run for every build, so a spec may interpolate
// them without declaring anything.
var reservedArgs = []string{"platform", "version"}

// UndeclaredArgs returns the names a spec interpolates that nothing declares,
// sorted and deduplicated.
//
// Interpolate deliberately leaves an unknown $name in the string rather than
// blanking it, so a typo shows up as a visibly wrong path instead of a silently
// truncated one. That is the right behaviour once a build is running and a poor
// one before it starts: nothing fails until the step that carries the reference
// executes, and what surfaces then is the invoked tool's complaint about a
// nonsensical argument rather than anything pointing back at the spec.
//
// This is the same reasoning that makes an undeclared operand in an ordered `if`
// an error rather than a silent false. It is reported rather than enforced here
// because a literal dollar sign is not always a mistake: a step may pass one to
// a tool that has its own idea of what it means.
func UndeclaredArgs(spec *Spec) []string {
	if spec == nil {
		return nil
	}

	declared := make(map[string]bool, len(spec.Args)+len(reservedArgs))
	for name := range spec.Args {
		declared[name] = true
	}
	for _, name := range reservedArgs {
		declared[name] = true
	}

	used := map[string]bool{}
	for _, steps := range [][]Step{spec.Steps, spec.UninstallSteps} {
		for _, step := range steps {
			collectRefs(step, used)
		}
	}

	var out []string
	for name := range used {
		if !declared[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// collectRefs gathers every $name a step interpolates. It reads the step's
// original JSON when there is any, so fields belonging to a host-registered step
// type are covered as well as the builtin ones — interpolation applies to all of
// them, so a check that only knew about Step's own fields would miss exactly the
// steps a host had to write itself.
func collectRefs(step Step, out map[string]bool) {
	if len(step.Raw) > 0 {
		var doc any
		if err := json.Unmarshal(step.Raw, &doc); err == nil {
			refsInJSON(doc, out)
			return
		}
	}
	// A spec built in Go rather than parsed from a file has no Raw.
	for _, s := range append([]string{
		step.If, step.From, step.Src, step.Dest, step.Cmd,
		step.URL, step.Path, step.Executable, step.Title,
	}, step.Args...) {
		refsIn(s, out)
	}
	for k, v := range step.Env {
		refsIn(k, out)
		refsIn(v, out)
	}
}

func refsInJSON(v any, out map[string]bool) {
	switch t := v.(type) {
	case string:
		refsIn(t, out)
	case []any:
		for _, e := range t {
			refsInJSON(e, out)
		}
	case map[string]any:
		for _, e := range t {
			refsInJSON(e, out)
		}
	}
}

func refsIn(s string, out map[string]bool) {
	for _, m := range argRe.FindAllStringSubmatch(s, -1) {
		out[m[1]+m[2]] = true // exactly one of the two groups matched
	}
}
