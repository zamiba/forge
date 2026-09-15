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
		declared["args."+name] = true
	}
	for _, name := range reservedArgs {
		declared[name] = true
	}
	// A variable bound by any entry of either table counts as declared. A step
	// using one that only some platforms bind is legitimate — it runs for those
	// platforms — so the union is the right set to accept.
	for _, name := range varNames(spec.PlatformVars) {
		declared["platform."+name] = true
	}
	for _, name := range varNames(spec.VersionVars) {
		declared["version."+name] = true
	}

	used, _ := collectSpecRefs(spec)

	var out []string
	for name := range used {
		// A provider reference is declared by the host, not the spec, so a
		// spec-only check cannot know whether ${romPath} resolves. ProviderRefs
		// names them for a host that can.
		if _, _, isProvider := providerRef(name); isProvider {
			continue
		}
		if !declared[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// ProviderRefs returns the names of the providers a spec reads through
// ${<name>Path} references, sorted and deduplicated. The spec cannot declare
// these; the host registers them, and this is how it checks before a run that it
// has registered every one the spec will ask for. A reference to one it has not
// fails the step that carries it, which is late for the same reason an
// undeclared argument is.
func ProviderRefs(spec *Spec) []string {
	if spec == nil {
		return nil
	}
	used, _ := collectSpecRefs(spec)
	names := map[string]bool{}
	for ref := range used {
		if provider, _, ok := providerRef(ref); ok {
			names[provider] = true
		}
	}
	return sortedKeys(names)
}

// collectRefs gathers every $name a step interpolates. It reads the step's
// original JSON when there is any, so fields belonging to a host-registered step
// type are covered as well as the builtin ones — interpolation applies to all of
// them, so a check that only knew about Step's own fields would miss exactly the
// steps a host had to write itself.
// UnbracedRefs returns the names a spec references with the superseded $name
// syntax, sorted and deduplicated.
//
// Braces are required, so an unbraced reference is not interpolated at all. That
// is quiet in the worst way: "$platform == Windows" compares the literal text
// against "Windows", is false forever, and skips its step without a word. These
// cannot be folded into UndeclaredArgs, because the names most likely to appear
// this way — platform and version — are declared, and would be filtered out
// precisely when reporting them matters most.
func UnbracedRefs(spec *Spec) []string {
	if spec == nil {
		return nil
	}
	_, bare := collectSpecRefs(spec)
	return sortedKeys(bare)
}

// collectSpecRefs walks everything in a spec that gets interpolated, returning
// the braced references and the unbraced ones separately.
func collectSpecRefs(spec *Spec) (braced, bare map[string]bool) {
	braced, bare = map[string]bool{}, map[string]bool{}
	for _, steps := range [][]Step{spec.Steps, spec.UninstallSteps} {
		for _, step := range steps {
			collectRefs(step, braced, bare)
		}
	}
	// User data paths are interpolated like any other path, so a typo in one is
	// the same class of mistake and worth catching in the same place.
	for _, p := range spec.UserDataPaths {
		refsIn(p, braced, bare)
	}
	return braced, bare
}

func sortedKeys(set map[string]bool) []string {
	var out []string
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func collectRefs(step Step, braced, bare map[string]bool) {
	if len(step.Raw) > 0 {
		var doc any
		if err := json.Unmarshal(step.Raw, &doc); err == nil {
			refsInJSON(doc, braced, bare)
			return
		}
	}
	// A spec built in Go rather than parsed from a file has no Raw.
	for _, s := range append([]string{
		step.If, step.From, step.Src, step.Dest, step.Cmd,
		step.URL, step.Path, step.Executable, step.Title,
	}, step.Args...) {
		refsIn(s, braced, bare)
	}
	for k, v := range step.Env {
		refsIn(k, braced, bare)
		refsIn(v, braced, bare)
	}
}

func refsInJSON(v any, braced, bare map[string]bool) {
	switch t := v.(type) {
	case string:
		refsIn(t, braced, bare)
	case []any:
		for _, e := range t {
			refsInJSON(e, braced, bare)
		}
	case map[string]any:
		for _, e := range t {
			refsInJSON(e, braced, bare)
		}
	}
}

func refsIn(s string, braced, bare map[string]bool) {
	for _, m := range argRe.FindAllStringSubmatch(s, -1) {
		braced[m[1]] = true
	}
	// Strip the braced references first, or the bare pattern matches the "$"
	// that starts each of them.
	for _, m := range bareArgRe.FindAllStringSubmatch(argRe.ReplaceAllString(s, ""), -1) {
		bare[m[1]] = true
	}
}
