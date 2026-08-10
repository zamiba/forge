package engine

import (
	"fmt"
	"sort"
)

// Prompt is one user-facing question a host must answer before a spec can run.
type Prompt struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Label   string      `json:"label"`
	Default string      `json:"default,omitempty"`
	Options []ArgOption `json:"options,omitempty"`
}

// Prompts returns the spec's args as an ordered list of prompts. Order is by
// arg name so a UI renders them consistently between runs.
func (s *Spec) Prompts() []Prompt {
	if s == nil || len(s.Args) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.Args))
	for name := range s.Args {
		names = append(names, name)
	}
	sort.Strings(names)

	prompts := make([]Prompt, 0, len(names))
	for _, name := range names {
		a := s.Args[name]
		prompts = append(prompts, Prompt{
			Name:    name,
			Type:    a.Type,
			Label:   a.Label,
			Default: a.Default,
			Options: a.Options,
		})
	}
	return prompts
}

// ResolveArgs validates supplied arg values against the spec and fills in
// defaults. A choice arg with no value and no default is an error, since a step
// referencing it would otherwise interpolate to a literal "$name".
func (s *Spec) ResolveArgs(supplied map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(s.Args))
	if s == nil {
		return out, nil
	}
	for name, spec := range s.Args {
		v, ok := supplied[name]
		if !ok || v == "" {
			v = spec.Default
		}
		if v == "" {
			if spec.Type == "string" {
				out[name] = ""
				continue
			}
			return nil, fmt.Errorf("missing value for arg %q (%s)", name, spec.Label)
		}
		if spec.Type == "choice" && len(spec.Options) > 0 {
			valid := false
			for _, o := range spec.Options {
				if o.Value == v {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("invalid value %q for arg %q", v, name)
			}
		}
		out[name] = v
	}
	// Values the spec does not declare are passed through rather than
	// rejected, so hosts can inject their own variables.
	for k, v := range supplied {
		if _, declared := s.Args[k]; !declared {
			out[k] = v
		}
	}
	return out, nil
}
