package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The object form's header supplies defaults; a build's own value replaces one.
func TestParseSpecFileHeaderDefaults(t *testing.T) {
	f, err := ParseSpecFile([]byte(`{
		"defaultVersion": "1.0.0",
		"dependencies": ["git", "cmake"],
		"buildPaths": ["build"],
		"builds": [
			{"versions": ["1.0.0"], "targetPlatforms": ["Linux"], "steps": []},
			{"versions": ["2.0.0"], "dependencies": ["ninja"], "steps": []}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if f.DefaultVersion != "1.0.0" {
		t.Errorf("DefaultVersion = %q", f.DefaultVersion)
	}
	if got := f.Specs[0].Dependencies; len(got) != 2 || got[0] != "git" {
		t.Errorf("build 0 dependencies = %v, want the header's", got)
	}
	if got := f.Specs[1].Dependencies; len(got) != 1 || got[0] != "ninja" {
		t.Errorf("build 1 dependencies = %v, want its own, replacing the header's", got)
	}
	// buildPaths is defaulted onto every build, including one that overrode a
	// different field.
	for i, sp := range f.Specs {
		if len(sp.BuildPaths) != 1 || sp.BuildPaths[0] != "build" {
			t.Errorf("build %d buildPaths = %v", i, sp.BuildPaths)
		}
	}
}

// An explicitly empty value means "none", not "inherit the header's".
func TestParseSpecFileEmptyOverridesHeader(t *testing.T) {
	f, err := ParseSpecFile([]byte(`{
		"dependencies": ["git"],
		"builds": [{"versions": ["1.0.0"], "dependencies": [], "steps": []}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Specs[0].Dependencies; got == nil || len(got) != 0 {
		t.Errorf("dependencies = %v, want an empty non-nil slice", got)
	}
}

func TestParseSpecFileBareArrayStillWorks(t *testing.T) {
	f, err := ParseSpecFile([]byte(`[{"version": "1.0.2", "targetPlatforms": ["Linux"], "steps": []}]`))
	if err != nil {
		t.Fatal(err)
	}
	if f.DefaultVersion != "" {
		t.Errorf("DefaultVersion = %q, want empty for the bare array form", f.DefaultVersion)
	}
	// The superseded scalar is folded into the list so nothing downstream has to
	// handle both.
	if got := f.Specs[0].Versions; len(got) != 1 || got[0] != "1.0.2" {
		t.Errorf("Versions = %v, want the scalar folded in", got)
	}
}

func TestParseSpecFileRejects(t *testing.T) {
	cases := map[string]string{
		"object with no builds": `{"dependencies": ["git"]}`,
		"undeclared default":    `{"defaultVersion": "9.9.9", "builds": [{"versions": ["1.0.0"], "steps": []}]}`,
		"malformed":             `{"builds": [`,
	}
	for name, body := range cases {
		if _, err := ParseSpecFile([]byte(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSelectAndVersionsAcrossVersionArrays(t *testing.T) {
	f, err := ParseSpecFile([]byte(`{
		"builds": [
			{"versions": ["1.0.0", "1.1.0"], "targetPlatforms": ["Linux", "Windows"], "steps": []},
			{"versions": ["2.0.0"], "targetPlatforms": ["Linux"], "steps": []}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// Both versions of the first build are selectable, on both its platforms.
	for _, v := range []string{"1.0.0", "1.1.0"} {
		for _, p := range []string{"Linux", "Windows"} {
			if got := Select(f.Specs, p, v); got == nil {
				t.Errorf("Select(%s, %s) = nil", p, v)
			}
		}
	}
	if got := Select(f.Specs, "Windows", "2.0.0"); got != nil {
		t.Error("2.0.0 has no Windows build but Select found one")
	}

	order := VersionOrder(f.Specs)
	want := []string{"1.0.0", "1.1.0", "2.0.0"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("VersionOrder = %v, want %v (declaration order)", order, want)
	}

	declared := Versions(f.Specs)
	if len(declared[0].Platforms) != 2 || len(declared[2].Platforms) != 1 {
		t.Errorf("platforms per version = %v", declared)
	}
}

// Ordered comparison is by position in the declared hierarchy, so version
// strings that no parser could rank still work.
func TestEvalConditionOrdered(t *testing.T) {
	order := []string{"1.0.2", "1.1 RC4", "Barnard Alfa"}
	cases := []struct {
		expr string
		want bool
	}{
		{"1.1 RC4 > 1.0.2", true},
		{"1.0.2 > 1.1 RC4", false},
		{"1.0.2 >= 1.0.2", true},
		{"1.0.2 < Barnard Alfa", true},
		{"Barnard Alfa <= 1.1 RC4", false},
		// Equality needs no ordering, so undeclared operands are fine here.
		{"anything == anything", true},
		{"1.0.2 != 1.1 RC4", true},
	}
	for _, c := range cases {
		got, err := EvalCondition(c.expr, nil, order)
		if err != nil {
			t.Errorf("%q: %v", c.expr, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q = %v, want %v", c.expr, got, c.want)
		}
	}
}

// A threshold naming a version that does not exist is an error, never a silent
// false — that would disable a build step with nothing to show for it.
func TestEvalConditionUndeclaredOperandErrors(t *testing.T) {
	order := []string{"1.0.0", "2.0.0"}
	for _, expr := range []string{"1.0.0 >= 1.5.0", "typo < 2.0.0"} {
		_, err := EvalCondition(expr, nil, order)
		if err == nil {
			t.Errorf("%q: expected an error", expr)
			continue
		}
		if !strings.Contains(err.Error(), "not a declared version") {
			t.Errorf("%q: unhelpful error %v", expr, err)
		}
	}
}

func TestEvalConditionInterpolatesBeforeComparing(t *testing.T) {
	order := []string{"1.0.0", "1.1.0", "1.2.0"}
	args := map[string]string{"version": "1.1.0"}
	for expr, want := range map[string]bool{
		"$version >= 1.1.0": true,
		"$version > 1.1.0":  false,
		"$version < 1.2.0":  true,
	} {
		got, err := EvalCondition(expr, args, order)
		if err != nil {
			t.Fatalf("%q: %v", expr, err)
		}
		if got != want {
			t.Errorf("%q = %v, want %v", expr, got, want)
		}
	}
}

// $platform is the axis that varies inside one build, so it must reach the steps
// and the conditions that branch on it.
func TestRunInjectsReservedArgs(t *testing.T) {
	dir := t.TempDir()
	steps := []Step{
		{Step: "createDir", Path: "out-$platform-$version"},
		{Step: "touch", If: "$platform == Windows", Path: "win-only"},
		{Step: "touch", If: "$platform != Windows", Path: "unix-only"},
	}
	if _, err := Run(t.Context(), Options{
		Steps:    steps,
		RootDir:  dir,
		Platform: "Linux",
		Version:  "1.0.2",
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"out-Linux-1.0.2", "unix-only"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("expected %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "win-only")); err == nil {
		t.Error("the Windows-only step ran on Linux")
	}
}

// The host's arg map must come back unmodified: PortForge persists it and
// replays it on rebuild, so a leaked $platform would fail the reserved check.
func TestRunDoesNotMutateCallerArgs(t *testing.T) {
	args := map[string]string{"renderer": "opengl"}
	if _, err := Run(t.Context(), Options{
		Steps:    []Step{{Step: "createDir", Path: "x"}},
		RootDir:  t.TempDir(),
		Args:     args,
		Platform: "Linux",
		Version:  "1.0.0",
	}); err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 {
		t.Errorf("caller's args were mutated: %v", args)
	}
}

func TestRunRejectsReservedArgNames(t *testing.T) {
	for _, name := range []string{"platform", "version"} {
		_, err := Run(t.Context(), Options{
			Steps:   []Step{{Step: "createDir", Path: "x"}},
			RootDir: t.TempDir(),
			Args:    map[string]string{name: "spoofed"},
		})
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("arg %q: got %v, want a reserved-name error", name, err)
		}
	}
}

// An unusable threshold must stop the run rather than quietly skipping a step.
func TestRunFailsOnBadCondition(t *testing.T) {
	_, err := Run(t.Context(), Options{
		Steps:        []Step{{Step: "createDir", If: "$version >= 9.9.9", Path: "x"}},
		RootDir:      t.TempDir(),
		Version:      "1.0.0",
		VersionOrder: []string{"1.0.0"},
	})
	if err == nil || !strings.Contains(err.Error(), "not a declared version") {
		t.Errorf("got %v, want a declared-version error", err)
	}
}
