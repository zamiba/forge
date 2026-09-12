package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// fileLevelOnly are the fields a build may not declare. They describe the item
// rather than any one way of building it, and more than one caller reads them —
// a host removing an item may have no uninstallSteps to consult, and protecting
// user data across a reinstall runs no uninstall sequence at all.
var fileLevelOnly = []string{"uninstallSteps", "userDataPaths"}

// selector is the parsed form of targetPlatforms or versions. Names are the
// values declared, in source order; Vars holds the variables each one binds,
// and is nil when the array form was used.
type selector struct {
	Names []string
	Vars  map[string]map[string]string
}

// decodeSelector accepts both forms of the field:
//
//	"targetPlatforms": ["Linux", "Windows"]
//	"targetPlatforms": { "Linux": {"slug": "linux-x86_64"}, "Windows": {"slug": "win64"} }
//
// The object form is read from the token stream rather than into a map, because
// Go randomises map iteration and for versions the declaration order *is* the
// version hierarchy that ordered conditions compare against.
func decodeSelector(raw json.RawMessage, field string) (selector, error) {
	var out selector
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return out, nil
	}

	if trimmed[0] == '[' {
		if err := json.Unmarshal(raw, &out.Names); err != nil {
			return out, fmt.Errorf("%s: %w", field, err)
		}
		return out, nil
	}
	if trimmed[0] != '{' {
		return out, fmt.Errorf("%s must be an array of names or an object mapping each name to its variables", field)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // consume '{'
		return out, fmt.Errorf("%s: %w", field, err)
	}
	out.Vars = map[string]map[string]string{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return out, fmt.Errorf("%s: %w", field, err)
		}
		name, _ := keyTok.(string)
		vars := map[string]string{}
		if err := dec.Decode(&vars); err != nil {
			return out, fmt.Errorf("%s[%q]: %w", field, name, err)
		}
		if _, dup := out.Vars[name]; dup {
			return out, fmt.Errorf("%s declares %q twice", field, name)
		}
		out.Names = append(out.Names, name)
		out.Vars[name] = vars
	}
	return out, nil
}

// UnmarshalJSON reads the two dual-form fields itself and hands the rest to the
// ordinary decoder. The alias type is what stops that from recursing.
func (s *Spec) UnmarshalJSON(data []byte) error {
	type alias Spec
	aux := struct {
		TargetPlatforms json.RawMessage `json:"targetPlatforms"`
		Versions        json.RawMessage `json:"versions"`
		*alias
	}{alias: (*alias)(s)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Teardown and user data belong to the file. Allowing them in both places
	// made it ambiguous which one ran, for two fields that in practice never
	// vary between builds: deletePath skips a path that is not there, so one
	// declaration naming every platform's and every version's leavings is
	// correct for all of them, and a genuine difference is an `if`.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err == nil {
		for _, field := range fileLevelOnly {
			if _, found := probe[field]; found {
				return fmt.Errorf("%s belongs to the file rather than a build: declare it once beside \"builds\"", field)
			}
		}
	}
	platforms, err := decodeSelector(aux.TargetPlatforms, "targetPlatforms")
	if err != nil {
		return err
	}
	versions, err := decodeSelector(aux.Versions, "versions")
	if err != nil {
		return err
	}
	s.TargetPlatforms, s.PlatformVars = platforms.Names, platforms.Vars
	s.Versions, s.VersionVars = versions.Names, versions.Vars
	return nil
}

// VarsFor returns the variables the given platform and version bind, for a host
// filling in Options. Either may be absent, which simply binds nothing.
func (s *Spec) VarsFor(platform, version string) (platformVars, versionVars map[string]string) {
	if s == nil {
		return nil, nil
	}
	return s.PlatformVars[platform], s.VersionVars[version]
}

// varNames lists every variable name a table binds, across all of its entries.
// A build whose platforms bind different sets is not an error here: a step that
// uses one only runs for the platforms that have it.
func varNames(table map[string]map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, vars := range table {
		for name := range vars {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// BuildOptions fills the spec-derived half of Options for an install: the
// steps, the declared dependencies, the variables the chosen platform and
// version bind, and the user data paths the run must protect.
//
// It exists because assembling those by hand is seven chances to forget one,
// and the consequence of forgetting the last is deleting a user's save files —
// which is exactly what forge's own CLI did until it was noticed. A host still
// sets RootDir and whatever else it needs, and may override anything here; the
// point is only that the spec-derived fields arrive complete, and that a field
// added later reaches every host rather than the ones that hear about it.
//
// args are used as given. Call ResolveArgs first to apply the spec's defaults.
func (s *Spec) BuildOptions(platform, version string, args map[string]string, order []string) Options {
	return s.options(s.Steps, platform, version, args, order)
}

// TeardownOptions is BuildOptions for removing an item instead of installing
// one: the uninstall sequence, Teardown set so user data is protected by
// deletePath rather than moved aside, and the dependency check skipped, since
// the tools that built an item may be long gone by the time it is removed.
func (s *Spec) TeardownOptions(platform, version string, args map[string]string, order []string) Options {
	opts := s.options(s.UninstallSteps, platform, version, args, order)
	opts.Teardown = true
	opts.SkipDependencyCheck = true
	return opts
}

func (s *Spec) options(steps []Step, platform, version string, args map[string]string, order []string) Options {
	platformVars, versionVars := s.VarsFor(platform, version)
	return Options{
		Steps:         steps,
		Dependencies:  s.Dependencies,
		Args:          args,
		PlatformVars:  platformVars,
		VersionVars:   versionVars,
		PreservePaths: s.UserDataPaths,
		Platform:      platform,
		Version:       version,
		VersionOrder:  order,
	}
}
