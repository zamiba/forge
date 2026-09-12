package engine

import (
	"reflect"
	"testing"
)

func specFromJSON(t *testing.T, doc string) *Spec {
	t.Helper()
	file, err := ParseSpecFile([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(file.Specs) != 1 {
		t.Fatalf("expected one build, got %d", len(file.Specs))
	}
	return &file.Specs[0]
}

func TestUndeclaredArgsFindsWhatNothingDeclares(t *testing.T) {
	// The shape that shipped broken: one arg declared, a second interpolated.
	spec := specFromJSON(t, `{
	  "args": {"textureMod": {"type": "choice", "label": "Texture mod"}},
	  "builds": [{
	    "versions": ["1.0"],
	    "steps": [
	      {"step": "make", "args": ["VERSION=${args.romVersion}", "MOD=${args.textureMod}"]},
	      {"step": "move", "src": "build/${args.romVersion}_pc", "dest": "install"}
	    ]
	  }]
	}`)

	got := UndeclaredArgs(spec)
	if want := []string{"args.romVersion"}; !reflect.DeepEqual(got, want) {
		t.Errorf("UndeclaredArgs = %v, want %v", got, want)
	}
}

// ${platform} and ${version} are injected for every run, so using them without an
// args entry is correct rather than an oversight.
func TestUndeclaredArgsAllowsInjectedNames(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "versions": ["1.0"],
	  "steps": [
	    {"step": "fetch", "url": "https://example.test/${version}/x-${platform}.zip", "dest": "a.zip"},
	    {"step": "run", "cmd": "sh", "if": "${platform} == Linux"}
	  ]
	}]`)

	if got := UndeclaredArgs(spec); len(got) != 0 {
		t.Errorf("UndeclaredArgs = %v, want none", got)
	}
}

// uninstallSteps are interpolated the same way steps are, and are the ones least
// likely to be exercised before a release.
func TestUndeclaredArgsCoversUninstallSteps(t *testing.T) {
	spec := specFromJSON(t, `{
	  "uninstallSteps": [{"step": "deletePath", "path": "install/${args.flavour}"}],
	  "builds": [{
	    "versions": ["1.0"],
	    "steps": [{"step": "createDir", "path": "install"}]
	  }]
	}`)

	if got := UndeclaredArgs(spec); len(got) != 1 || got[0] != "args.flavour" {
		t.Errorf("UndeclaredArgs = %v, want [args.flavour]", got)
	}
}

// A host's own step type carries fields the builtin Step struct has never heard
// of, and those are interpolated too. Reading the step's original JSON is what
// makes them visible; a check over Step's own fields would miss precisely the
// steps a host had to write itself.
func TestUndeclaredArgsSeesHostStepFields(t *testing.T) {
	spec := specFromJSON(t, `[{
	  "versions": ["1.0"],
	  "steps": [{"step": "portforge:gogDownload", "productId": "${args.gogId}", "into": "depot"}]
	}]`)

	if got := UndeclaredArgs(spec); len(got) != 1 || got[0] != "args.gogId" {
		t.Errorf("UndeclaredArgs = %v, want [args.gogId]", got)
	}
}

// A spec assembled in Go has no original JSON to read, so the builtin fields
// are the fallback.
func TestUndeclaredArgsWithoutRawJSON(t *testing.T) {
	spec := &Spec{Steps: []Step{
		{Step: "move", Src: "build/${args.flavour}", Dest: "install"},
		{Step: "run", Cmd: "sh", Args: []string{"-c", "echo ${args.greeting}"}},
		{Step: "run", Cmd: "sh", Env: map[string]string{"OUT": "${args.flavour}"}},
	}}

	got := UndeclaredArgs(spec)
	if want := []string{"args.flavour", "args.greeting"}; !reflect.DeepEqual(got, want) {
		t.Errorf("UndeclaredArgs = %v, want %v", got, want)
	}
}

func TestUndeclaredArgsOnACleanSpec(t *testing.T) {
	spec := specFromJSON(t, `{
	  "args": {"region": {"type": "string", "label": "Region"}},
	  "builds": [{
	    "versions": ["1.0"],
	    "steps": [{"step": "move", "src": "build/${args.region}_pc", "dest": "install"}]
	  }]
	}`)

	if got := UndeclaredArgs(spec); got != nil {
		t.Errorf("UndeclaredArgs = %v, want nil", got)
	}
}

func TestUndeclaredArgsHandlesNil(t *testing.T) {
	if got := UndeclaredArgs(nil); got != nil {
		t.Errorf("UndeclaredArgs(nil) = %v, want nil", got)
	}
}
