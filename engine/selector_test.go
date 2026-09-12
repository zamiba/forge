package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSelectorAcceptsTheArrayForm(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "versions": ["1.0", "2.0"],
	  "targetPlatforms": ["Linux", "Windows"],
	  "steps": []
	}]`)
	if !reflect.DeepEqual(spec.Versions, []string{"1.0", "2.0"}) {
		t.Errorf("versions = %v", spec.Versions)
	}
	if !reflect.DeepEqual(spec.TargetPlatforms, []string{"Linux", "Windows"}) {
		t.Errorf("targetPlatforms = %v", spec.TargetPlatforms)
	}
	if spec.PlatformVars != nil || spec.VersionVars != nil {
		t.Error("the array form binds no variables")
	}
}

// Declaration order across the file is the version hierarchy, so an object's
// key order has to survive the decode. Go randomises map iteration, which is
// why this is read from the token stream.
func TestSelectorObjectFormPreservesDeclarationOrder(t *testing.T) {
	for i := 0; i < 20; i++ {
		spec := specFromJSON(t, `[{
		  "versions": {
		    "1.2": {"tag": "v1.2"},
		    "1.10": {"tag": "v1.10"},
		    "1.11 RC4": {"tag": "v1.11-rc4"},
		    "1.11": {"tag": "v1.11"}
		  },
		  "steps": []
		}]`)
		want := []string{"1.2", "1.10", "1.11 RC4", "1.11"}
		if !reflect.DeepEqual(spec.Versions, want) {
			t.Fatalf("pass %d: versions = %v, want %v", i, spec.Versions, want)
		}
	}
}

func TestSelectorObjectFormBindsVariables(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "versions": {"1.1 RC4": {"tag": "v1.1-rc4"}},
	  "targetPlatforms": {
	    "Linux-x64": {"slug": "linux-x86_64", "exe": "game"},
	    "Mac":       {"slug": "macos", "exe": "game.app"}
	  },
	  "steps": []
	}]`)

	pv, vv := spec.VarsFor("Mac", "1.1 RC4")
	if pv["slug"] != "macos" || pv["exe"] != "game.app" {
		t.Errorf("platform vars = %v", pv)
	}
	if vv["tag"] != "v1.1-rc4" {
		t.Errorf("version vars = %v", vv)
	}
	if pv, _ := spec.VarsFor("Linux-x64", ""); pv["slug"] != "linux-x86_64" {
		t.Errorf("linux slug = %q", pv["slug"])
	}
}

// The whole point of the table: an upstream name that cannot be derived from
// the platform or version reaches the steps as a variable.
func TestRunSubstitutesTableVariables(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "versions": {"Barnard Alfa": {"tag": "v2.0.0", "infix": "Barnard-Alfa"}},
	  "targetPlatforms": {"Mac-x64": {"slug": "mac-intel-x64"}},
	  "steps": []
	}]`)
	pv, vv := spec.VarsFor("Mac-x64", "Barnard Alfa")

	root := t.TempDir()
	if _, err := run(t, Options{
		RootDir:      root,
		Platform:     "Mac-x64",
		Version:      "Barnard Alfa",
		PlatformVars: pv,
		VersionVars:  vv,
		Steps: []Step{
			{Step: "touch", Path: "${version.tag}-${version.infix}-${platform.slug}"},
			// Bare names still resolve to the selected platform and version
			// themselves, which is what the older conditions rely on.
			{Step: "touch", Path: "bare-${platform}", If: "${platform} == Mac-x64"},
		},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"v2.0.0-Barnard-Alfa-mac-intel-x64", "bare-Mac-x64"} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("expected %s: %v", want, err)
		}
	}
}

func TestSelectorRejectsDuplicateAndMalformedEntries(t *testing.T) {
	if _, err := ParseSpecFile([]byte(`[{"versions": "1.0", "steps": []}]`)); err == nil {
		t.Error("a bare string should not be accepted")
	}
	_, err := ParseSpecFile([]byte(`[{
	  "targetPlatforms": {"Linux": {"slug": "a"}, "Linux": {"slug": "b"}},
	  "steps": []
	}]`))
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("duplicate key should be rejected, got %v", err)
	}
}

func TestUndeclaredArgsAcceptsTableVariables(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "versions": {"1.0": {"tag": "v1.0"}},
	  "targetPlatforms": {"Linux": {"slug": "linux"}, "Mac": {"slug": "macos", "exe": "x.app"}},
	  "steps": [
	    {"step": "fetch", "url": "https://e.test/${version.tag}/g-${platform.slug}.zip", "dest": "a.zip"},
	    {"step": "defineExecutable", "executable": "install/${platform.exe}", "title": "Play"}
	  ]
	}]`)
	if got := UndeclaredArgs(spec); len(got) != 0 {
		t.Errorf("UndeclaredArgs = %v, want none — exe is bound by one platform, which is legitimate", got)
	}
}

func TestUndeclaredArgsReportsUnknownTableVariables(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "targetPlatforms": {"Linux": {"slug": "linux"}},
	  "steps": [{"step": "touch", "path": "${platform.nope}"}]
	}]`)
	if got := UndeclaredArgs(spec); len(got) != 1 || got[0] != "platform.nope" {
		t.Errorf("UndeclaredArgs = %v, want [platform.nope]", got)
	}
}

// Braces are required now, so an unbraced reference is inert. It must be
// reported rather than quietly doing nothing.
func TestUndeclaredArgsReportsTheUnbracedForm(t *testing.T) {
	spec := specFromJSON(t, `{
	  "args": {"region": {"type": "string", "label": "Region"}},
	  "builds": [{
	    "versions": ["1.0"],
	    "steps": [{"step": "touch", "path": "out-$region-$platform"}]
	  }]
	}`)
	// Neither is reported as undeclared: region is declared, platform is
	// reserved. Both are inert, which is why they need their own report.
	if got := UndeclaredArgs(spec); len(got) != 0 {
		t.Errorf("UndeclaredArgs = %v, want none", got)
	}
	got := UnbracedRefs(spec)
	if want := []string{"platform", "region"}; !reflect.DeepEqual(got, want) {
		t.Errorf("UnbracedRefs = %v, want %v", got, want)
	}
}

func TestUnbracedRefsIgnoresProperReferences(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "targetPlatforms": {"Linux": {"slug": "linux"}},
	  "steps": [{"step": "touch", "path": "${platform.slug}-${version}-${platform}"}]
	}]`)
	if got := UnbracedRefs(spec); len(got) != 0 {
		t.Errorf("UnbracedRefs = %v, want none", got)
	}
}

func TestInterpolateIgnoresTheUnbracedForm(t *testing.T) {
	args := map[string]string{"region": "us"}
	if got := Interpolate("$region", args); got != "$region" {
		t.Errorf("Interpolate($region) = %q, want it left alone", got)
	}
	if got := Interpolate("${region}", args); got != "us" {
		t.Errorf("Interpolate(${region}) = %q, want us", got)
	}
}

func TestVersionOrderFollowsObjectDeclarationOrder(t *testing.T) {
	file, err := ParseSpecFile([]byte(`{
	  "builds": [{
	    "versions": {"1.2": {"tag": "1.2"}, "1.10": {"tag": "1.10"}},
	    "steps": []
	  }]
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := []string{"1.2", "1.10"}; !reflect.DeepEqual(VersionOrder(file.Specs), want) {
		t.Errorf("VersionOrder = %v, want %v", VersionOrder(file.Specs), want)
	}
}

// A teardown belongs to the file. Accepting it in both places left it ambiguous
// which one ran, for a field that never actually varied between builds.
func TestUninstallStepsAreRejectedOnABuild(t *testing.T) {
	_, err := ParseSpecFile([]byte(`[{
	  "versions": ["1.0"],
	  "steps": [],
	  "uninstallSteps": [{"step": "deletePath", "path": "install"}]
	}]`))
	if err == nil {
		t.Fatal("a build declaring uninstallSteps should be rejected")
	}
	if !strings.Contains(err.Error(), "belongs to the file") {
		t.Errorf("the error should say where it goes instead: %v", err)
	}
}

func TestUninstallStepsReachEveryBuildFromTheFile(t *testing.T) {
	file, err := ParseSpecFile([]byte(`{
	  "uninstallSteps": [{"step": "deletePath", "path": "install"}],
	  "builds": [
	    {"versions": ["1.0"], "targetPlatforms": ["Linux"], "steps": []},
	    {"versions": ["1.0"], "targetPlatforms": ["Windows"], "steps": []}
	  ]
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for i := range file.Specs {
		if len(file.Specs[i].UninstallSteps) != 1 {
			t.Errorf("build %d got %d uninstall steps, want 1", i, len(file.Specs[i].UninstallSteps))
		}
	}
}
