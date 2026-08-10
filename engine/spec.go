package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
)

// Spec is one entry in an install spec file. A spec file contains an array of
// these; the first whose TargetPlatforms matches the host is the one that runs.
type Spec struct {
	Version         string             `json:"version,omitempty"`
	TargetPlatforms []string           `json:"targetPlatforms,omitempty"`
	Dependencies    []string           `json:"dependencies,omitempty"`
	Args            map[string]ArgSpec `json:"args,omitempty"`
	Steps           []Step             `json:"steps"`
	BuildPaths      []string           `json:"buildPaths,omitempty"`
	UninstallSteps  []Step             `json:"uninstallSteps,omitempty"`
}

// ArgSpec describes a single user-configurable install argument.
type ArgSpec struct {
	Type    string      `json:"type"` // "choice" | "string"
	Label   string      `json:"label"`
	Default string      `json:"default,omitempty"`
	Options []ArgOption `json:"options,omitempty"` // for type "choice"
}

// ArgOption is one selectable value for a choice arg. Fields beyond value and
// label are preserved in Extra so hosts can attach their own metadata — PortForge
// uses this to tag options with the ROM they require.
type ArgOption struct {
	Value string          `json:"value"`
	Label string          `json:"label"`
	Extra json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps the full option object around in Extra so host-specific
// fields survive a decode/encode round trip.
func (o *ArgOption) UnmarshalJSON(data []byte) error {
	type plain ArgOption
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*o = ArgOption(p)
	o.Extra = append(json.RawMessage(nil), data...)
	return nil
}

// Step is a single step in a build sequence. The Step field selects the handler;
// the remaining fields are the union of what the builtin handlers read. Custom
// handlers registered by a host read their own fields out of Raw.
type Step struct {
	Step string `json:"step"`
	If   string `json:"if,omitempty"` // skipped when it evaluates false

	// copy
	From string `json:"from,omitempty"` // provider name; empty means a local copy
	Src  string `json:"src,omitempty"`
	Dest string `json:"dest,omitempty"`

	// run / make
	Cmd  string            `json:"cmd,omitempty"`
	Args []string          `json:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty"`

	// fetch
	URL string `json:"url,omitempty"`

	// cd, createDir, touch, deletePath
	Path string `json:"path,omitempty"`

	// defineExecutable
	Executable string `json:"executable,omitempty"`
	Title      string `json:"title,omitempty"`

	// Raw is the undecoded step object, so custom step handlers can pull out
	// fields the builtin Step struct knows nothing about.
	Raw json.RawMessage `json:"-"`
}

func (s *Step) UnmarshalJSON(data []byte) error {
	type plain Step
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*s = Step(p)
	s.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// DecodeRaw unmarshals the step's original JSON into v, giving custom step
// handlers access to fields outside the builtin Step struct.
func (s Step) DecodeRaw(v any) error {
	if len(s.Raw) == 0 {
		return fmt.Errorf("step %q has no raw JSON", s.Step)
	}
	return json.Unmarshal(s.Raw, v)
}

// Executable is a launch target declared by a defineExecutable step. Path is
// relative to the run's root directory.
type Executable struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// ParseSpecs decodes a spec file's contents. The file is a JSON array of specs.
func ParseSpecs(data []byte) ([]Spec, error) {
	var specs []Spec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	return specs, nil
}

// LoadSpecFile reads and decodes a spec file. A missing file is not an error —
// it returns a nil slice, matching "this item has no install spec".
func LoadSpecFile(path string) ([]Spec, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSpecs(data)
}

// Select returns the first spec whose TargetPlatforms contains platform, or has
// no platform restriction at all. Returns nil when none match.
func Select(specs []Spec, platform string) *Spec {
	for i := range specs {
		s := &specs[i]
		if len(s.TargetPlatforms) == 0 {
			return s
		}
		for _, p := range s.TargetPlatforms {
			if p == platform {
				return s
			}
		}
	}
	return nil
}

// HostPlatform returns the current OS as the platform string used in
// targetPlatforms: "Windows", "Mac" or "Linux".
func HostPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "Mac"
	default:
		return "Linux"
	}
}
