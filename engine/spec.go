package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
)

// Spec is one build in a spec file: the versions and platforms it covers, and
// the steps that produce them. A build spanning both axes asserts that every
// combination of the two is buildable, so a ragged matrix — a version that ships
// for fewer platforms than its siblings — needs a build of its own.
type Spec struct {
	// Versions this build produces, oldest first. Declaration order across the
	// whole file is the version hierarchy that ordered `if` comparisons use.
	Versions []string `json:"versions,omitempty"`
	// Version is the superseded scalar form, still read so spec files written
	// before Versions existed keep working. Parsing folds it into Versions.
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

// SpecFile is a parsed spec file: the builds it declares plus the file-level
// settings that apply across them.
type SpecFile struct {
	// DefaultVersion is the version a host should offer first. Declaration order
	// is chronological, so "newest" and "recommended" are not the same thing —
	// a project whose newest release is a pre-release wants the stable one here.
	// Empty means the host decides, which in practice is the newest.
	DefaultVersion string
	Specs          []Spec
}

// specFileHeader is the object form of a spec file: defaults that apply to every
// build, plus the builds themselves. Its fields are pointers so an explicitly
// empty value in the file can be told apart from an absent one — a build that
// declares "dependencies": [] means it needs none, not that it inherits.
type specFileHeader struct {
	DefaultVersion string              `json:"defaultVersion"`
	Dependencies   *[]string           `json:"dependencies"`
	Args           *map[string]ArgSpec `json:"args"`
	BuildPaths     *[]string           `json:"buildPaths"`
	UninstallSteps *[]Step             `json:"uninstallSteps"`
	Builds         []Spec              `json:"builds"`
}

// ParseSpecFile decodes a spec file in either supported form: a bare JSON array
// of builds, or an object with file-level defaults and a "builds" array.
//
// Header values are defaults, and a build's own value replaces one — they are
// never merged. Only the declarative fields can be defaulted this way; step
// arrays never are, because merging two sequences has no obvious meaning and
// every scheme for it makes specs harder to read than the duplication does.
func ParseSpecFile(data []byte) (*SpecFile, error) {
	trimmed := bytes.TrimLeft(data, " \t\r\n")

	if len(trimmed) > 0 && trimmed[0] == '[' {
		var specs []Spec
		if err := json.Unmarshal(data, &specs); err != nil {
			return nil, fmt.Errorf("parse spec: %w", err)
		}
		normaliseSpecs(specs)
		return &SpecFile{Specs: specs}, nil
	}

	var h specFileHeader
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if h.Builds == nil {
		return nil, fmt.Errorf("parse spec: object form needs a \"builds\" array")
	}
	for i := range h.Builds {
		b := &h.Builds[i]
		if b.Dependencies == nil && h.Dependencies != nil {
			b.Dependencies = *h.Dependencies
		}
		if b.Args == nil && h.Args != nil {
			b.Args = *h.Args
		}
		if b.BuildPaths == nil && h.BuildPaths != nil {
			b.BuildPaths = *h.BuildPaths
		}
		if b.UninstallSteps == nil && h.UninstallSteps != nil {
			b.UninstallSteps = *h.UninstallSteps
		}
	}
	normaliseSpecs(h.Builds)

	if h.DefaultVersion != "" && !contains(VersionOrder(h.Builds), h.DefaultVersion) {
		return nil, fmt.Errorf("parse spec: defaultVersion %q is not declared by any build", h.DefaultVersion)
	}
	return &SpecFile{DefaultVersion: h.DefaultVersion, Specs: h.Builds}, nil
}

// normaliseSpecs folds the superseded scalar Version into Versions so everything
// downstream only has to handle the list form.
func normaliseSpecs(specs []Spec) {
	for i := range specs {
		s := &specs[i]
		if len(s.Versions) == 0 && s.Version != "" {
			s.Versions = []string{s.Version}
		}
	}
}

// ParseSpecs decodes a spec file and returns only its builds.
func ParseSpecs(data []byte) ([]Spec, error) {
	f, err := ParseSpecFile(data)
	if err != nil {
		return nil, err
	}
	return f.Specs, nil
}

// LoadSpecFile reads and decodes a spec file. A missing file is not an error —
// it returns nil, matching "this item has no install spec".
func LoadSpecFile(path string) (*SpecFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSpecFile(data)
}

// Select returns the first spec matching both platform and version, or nil when
// none match. A spec with no TargetPlatforms matches any platform. An empty
// version argument matches any spec, so callers that do not care about versions —
// or are reading a spec file that predates the version field — can pass "".
//
// Both axes are the caller's decision: the engine does not rank versions or
// prefer the host platform, because it has no basis to. Hosts choose what to
// build; Select only finds it.
// versionList is the build's versions in either form. Parsing folds the scalar
// into the list, but a Spec built in Go by a host may still carry only the
// scalar, so every reader goes through here.
func (s *Spec) versionList() []string {
	if len(s.Versions) > 0 {
		return s.Versions
	}
	if s.Version != "" {
		return []string{s.Version}
	}
	return nil
}

func Select(specs []Spec, platform, version string) *Spec {
	for i := range specs {
		s := &specs[i]
		if version != "" && !contains(s.versionList(), version) {
			continue
		}
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

// Versions returns the distinct versions declared across specs, in the order they
// appear, each with the union of the platforms its specs target. A spec with no
// TargetPlatforms contributes no platforms, since it targets all of them.
//
// Hosts use this to offer a version picker: the file is the source of truth for
// what can be built, and the order it declares is the order to display.
func Versions(specs []Spec) []SpecVersion {
	var out []SpecVersion
	index := map[string]int{}
	for i := range specs {
		s := &specs[i]
		for _, v := range s.versionList() {
			at, seen := index[v]
			if !seen {
				index[v] = len(out)
				out = append(out, SpecVersion{Version: v})
				at = len(out) - 1
			}
			for _, p := range s.TargetPlatforms {
				if !contains(out[at].Platforms, p) {
					out[at].Platforms = append(out[at].Platforms, p)
				}
			}
		}
	}
	return out
}

// VersionOrder returns just the version strings from Versions, which is the
// ordered hierarchy that `>` and `<` conditions resolve operands against.
func VersionOrder(specs []Spec) []string {
	declared := Versions(specs)
	out := make([]string, len(declared))
	for i, v := range declared {
		out[i] = v.Version
	}
	return out
}

// SpecVersion is one declared version of a spec file and the platforms it builds for.
type SpecVersion struct {
	Version   string   `json:"version"`
	Platforms []string `json:"platforms"`
	// Default marks the version a host should offer first. Only SpecFile.Versions
	// sets it, since the plain Versions function sees builds without the
	// file-level settings that name the default.
	Default bool `json:"default,omitempty"`
}

// Versions returns the file's declared versions with the default one marked.
// When the file names no default, the newest — the last declared, since the
// order is chronological — is marked instead, so a host always has one to offer.
func (f *SpecFile) Versions() []SpecVersion {
	out := Versions(f.Specs)
	if len(out) == 0 {
		return out
	}
	want := f.DefaultVersion
	if want == "" {
		want = out[len(out)-1].Version
	}
	for i := range out {
		if out[i].Version == want {
			out[i].Default = true
			break
		}
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
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
