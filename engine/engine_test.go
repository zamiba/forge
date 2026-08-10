package engine

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInterpolate(t *testing.T) {
	args := map[string]string{"romVersion": "us", "empty": ""}
	cases := []struct{ in, want string }{
		{"baserom.$romVersion.z64", "baserom.us.z64"},
		{"build/${romVersion}_pc", "build/us_pc"},
		{"no vars here", "no vars here"},
		{"$empty/x", "/x"},
		{"$unknown", "$unknown"}, // unknown names survive rather than blanking
		{"${unknown}/y", "${unknown}/y"},
	}
	for _, c := range cases {
		if got := Interpolate(c.in, args); got != c.want {
			t.Errorf("Interpolate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEvalCondition(t *testing.T) {
	args := map[string]string{"textureMod": "none", "hd": "hd"}
	cases := []struct {
		expr string
		want bool
	}{
		{"${textureMod} != none", false},
		{"${textureMod} == none", true},
		{"${hd} != none", true},
		{"", false},
		{"false", false},
		{"0", false},
		{"yes", true},
	}
	for _, c := range cases {
		if got := EvalCondition(c.expr, args); got != c.want {
			t.Errorf("EvalCondition(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestSelect(t *testing.T) {
	specs := []Spec{
		{TargetPlatforms: []string{"Windows"}, Version: "win"},
		{TargetPlatforms: []string{"Linux", "Mac"}, Version: "unix"},
	}
	if got := Select(specs, "Mac"); got == nil || got.Version != "unix" {
		t.Errorf("Select(Mac) = %v, want the unix spec", got)
	}
	if got := Select(specs, "Haiku"); got != nil {
		t.Errorf("Select(Haiku) = %v, want nil", got)
	}
	unrestricted := []Spec{{Version: "any"}}
	if got := Select(unrestricted, "Haiku"); got == nil || got.Version != "any" {
		t.Errorf("a spec with no targetPlatforms should match everything")
	}
}

// run is a helper that executes steps in a temp dir and returns the result.
func run(t *testing.T, opts Options) (*Result, error) {
	t.Helper()
	if opts.RootDir == "" {
		opts.RootDir = t.TempDir()
	}
	return Run(context.Background(), opts)
}

func TestRunFileSteps(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := run(t, Options{
		RootDir: root,
		Args:    map[string]string{"name": "game"},
		Steps: []Step{
			{Step: "createDir", Path: "build/${name}"},
			{Step: "copy", Src: "source.txt", Dest: "build/${name}/copied.txt"},
			{Step: "touch", Path: "build/${name}/marker"},
			{Step: "move", Src: "build/${name}", Dest: "install"},
			{Step: "deletePath", Path: "build"},
			{Step: "defineExecutable", Executable: "install/copied.txt", Title: "Play"},
		},
		RequireExecutable: true,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(root, "install", "copied.txt")); err != nil || string(got) != "hello" {
		t.Errorf("install/copied.txt = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "install", "marker")); err != nil {
		t.Errorf("marker not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "build")); !os.IsNotExist(err) {
		t.Errorf("build dir should have been deleted")
	}
	if len(res.Executables) != 1 || res.Executables[0].Path != "install/copied.txt" {
		t.Errorf("executables = %+v", res.Executables)
	}
	if res.Executables[0].Title != "Play" {
		t.Errorf("title = %q, want Play", res.Executables[0].Title)
	}
}

func TestDefineExecutableDefaultsTitleToFilename(t *testing.T) {
	root := t.TempDir()
	res, err := run(t, Options{
		RootDir: root,
		Steps:   []Step{{Step: "defineExecutable", Executable: "install/soh.appimage"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Executables[0].Title != "soh.appimage" {
		t.Errorf("title = %q, want soh.appimage", res.Executables[0].Title)
	}
}

func TestCdChangesWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	_, err := run(t, Options{
		RootDir: root,
		Steps: []Step{
			{Step: "createDir", Path: "a/b"},
			{Step: "cd", Path: "a/b"},
			{Step: "touch", Path: "inner"},
			{Step: "cd", Path: ".."},
			{Step: "touch", Path: "outer"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "b", "inner")); err != nil {
		t.Errorf("inner file not created in a/b: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "outer")); err != nil {
		t.Errorf("cd .. did not move up one level: %v", err)
	}
}

func TestConditionalStepsAreSkippedAndNotCounted(t *testing.T) {
	var starts, skips int
	var total int
	_, err := run(t, Options{
		Args: map[string]string{"mod": "none"},
		Steps: []Step{
			{Step: "createDir", Path: "always"},
			{Step: "createDir", Path: "never", If: "${mod} != none"},
			{Step: "createDir", Path: "also-always"},
		},
		Events: func(e Event) {
			switch e.Kind {
			case EventRunStart:
				total = e.Total
			case EventStepStart:
				starts++
			case EventStepSkip:
				skips++
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || starts != 2 || skips != 1 {
		t.Errorf("total=%d starts=%d skips=%d, want 2/2/1", total, starts, skips)
	}
}

func TestRunStepRequiresDeclaredDependency(t *testing.T) {
	_, err := run(t, Options{
		Dependencies:        []string{"echo"},
		SkipDependencyCheck: true,
		Steps:               []Step{{Step: "run", Cmd: "sh", Args: []string{"-c", "touch pwned"}}},
	})
	if err == nil {
		t.Fatal("expected an undeclared command to be rejected")
	}
	if !strings.Contains(err.Error(), "not declared in dependencies") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunStepAllowsDeclaredDependency(t *testing.T) {
	root := t.TempDir()
	_, err := run(t, Options{
		RootDir:      root,
		Dependencies: []string{"touch"},
		Steps:        []Step{{Step: "run", Cmd: "touch", Args: []string{"created-by-run"}}},
	})
	if err != nil {
		t.Fatalf("declared command should be allowed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "created-by-run")); err != nil {
		t.Errorf("run step did not execute in the working directory: %v", err)
	}
}

func TestUnknownStepTypeFails(t *testing.T) {
	_, err := run(t, Options{Steps: []Step{{Step: "rm -rf /"}}})
	if err == nil || !strings.Contains(err.Error(), "unknown step type") {
		t.Errorf("got %v, want an unknown step type error", err)
	}
}

func TestCustomStepHandler(t *testing.T) {
	called := ""
	_, err := run(t, Options{
		Steps: []Step{{Step: "subvolume", Path: "/mnt/games/foo"}},
		StepHandlers: map[string]StepFunc{
			"subvolume": func(_ context.Context, st *State, s Step) error {
				called = st.Interp(s.Path)
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != "/mnt/games/foo" {
		t.Errorf("custom handler got %q", called)
	}
}

func TestCopyUsesRegisteredProvider(t *testing.T) {
	root := t.TempDir()
	romDir := t.TempDir()
	romPath := filepath.Join(romDir, "baserom.z64")
	if err := os.WriteFile(romPath, []byte("ROM"), 0644); err != nil {
		t.Fatal(err)
	}

	var gotSrc string
	_, err := run(t, Options{
		RootDir: root,
		Args:    map[string]string{"region": "us"},
		Steps:   []Step{{Step: "copy", From: "rom", Src: "Super Mario 64 (${region})", Dest: "baserom.z64"}},
		Providers: map[string]Provider{
			"rom": ProviderFunc(func(_ context.Context, req ProviderRequest) (string, error) {
				gotSrc = req.Src
				return romPath, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotSrc != "Super Mario 64 (us)" {
		t.Errorf("provider received src %q, want args interpolated", gotSrc)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "baserom.z64")); string(got) != "ROM" {
		t.Errorf("rom not copied, got %q", got)
	}
}

func TestCopyFailsOnUnregisteredProvider(t *testing.T) {
	_, err := run(t, Options{
		Steps: []Step{{Step: "copy", From: "rom", Dest: "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no provider registered") {
		t.Errorf("got %v, want a missing provider error", err)
	}
}

func TestExtractRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "evil.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("pwned"))
	zw.Close()
	f.Close()

	_, err = run(t, Options{
		RootDir: root,
		Steps:   []Step{{Step: "extract", Src: "evil.zip", Dest: "out"}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid path in zip") {
		t.Errorf("got %v, want a path traversal rejection", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.txt")); !os.IsNotExist(err) {
		t.Error("traversal escaped the destination directory")
	}
}

func TestRequireExecutable(t *testing.T) {
	_, err := run(t, Options{
		Steps:             []Step{{Step: "createDir", Path: "x"}},
		RequireExecutable: true,
	})
	if err == nil || !strings.Contains(err.Error(), "no 'defineExecutable' steps") {
		t.Errorf("got %v, want a missing-executable error", err)
	}
	if _, err := run(t, Options{Steps: []Step{{Step: "createDir", Path: "x"}}}); err != nil {
		t.Errorf("without RequireExecutable the run should succeed: %v", err)
	}
}

func TestFailureEmitsRunFailed(t *testing.T) {
	var failed *Event
	_, err := run(t, Options{
		Steps: []Step{
			{Step: "createDir", Path: "ok"},
			{Step: "copy", Src: "does-not-exist", Dest: "x"},
		},
		Events: func(e Event) {
			if e.Kind == EventRunFailed {
				ev := e
				failed = &ev
			}
		},
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if failed == nil {
		t.Fatal("no run:failed event emitted")
	}
	if failed.Index != 1 || failed.Step != "copy" {
		t.Errorf("failed event = %+v, want index 1 step copy", failed)
	}
}

func TestCancellationStopsTheRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := t.TempDir()
	_, err := Run(ctx, Options{
		RootDir: root,
		Steps: []Step{
			{Step: "createDir", Path: "first"},
			{Step: "createDir", Path: "second"},
		},
		Events: func(e Event) {
			if e.Kind == EventStepDone {
				cancel() // cancel after the first step completes
			}
		},
	})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if _, statErr := os.Stat(filepath.Join(root, "second")); !os.IsNotExist(statErr) {
		t.Error("run continued past cancellation")
	}
}

func TestResolveArgs(t *testing.T) {
	spec := &Spec{Args: map[string]ArgSpec{
		"region": {Type: "choice", Label: "Region", Options: []ArgOption{{Value: "us"}, {Value: "eu"}}},
		"mod":    {Type: "choice", Label: "Mod", Default: "none", Options: []ArgOption{{Value: "none"}, {Value: "hd"}}},
	}}

	got, err := spec.ResolveArgs(map[string]string{"region": "eu"})
	if err != nil {
		t.Fatal(err)
	}
	if got["region"] != "eu" || got["mod"] != "none" {
		t.Errorf("ResolveArgs = %v, want region=eu and the mod default applied", got)
	}
	if _, err := spec.ResolveArgs(map[string]string{"region": "jp"}); err == nil {
		t.Error("expected an invalid choice to be rejected")
	}
	if _, err := spec.ResolveArgs(nil); err == nil {
		t.Error("expected a missing required arg to be rejected")
	}
}

func TestPromptsAreOrdered(t *testing.T) {
	spec := &Spec{Args: map[string]ArgSpec{
		"zulu":  {Type: "string", Label: "Z"},
		"alpha": {Type: "string", Label: "A"},
	}}
	p := spec.Prompts()
	if len(p) != 2 || p[0].Name != "alpha" || p[1].Name != "zulu" {
		t.Errorf("prompts = %+v, want alphabetical order", p)
	}
}

func TestStepRawPreservesUnknownFields(t *testing.T) {
	specs, err := ParseSpecs([]byte(`[{"steps":[{"step":"subvolume","path":"/x","compression":"zstd:3"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	var custom struct {
		Compression string `json:"compression"`
	}
	if err := specs[0].Steps[0].DecodeRaw(&custom); err != nil {
		t.Fatal(err)
	}
	if custom.Compression != "zstd:3" {
		t.Errorf("compression = %q, want zstd:3", custom.Compression)
	}
}

func TestPathsAreConfinedToRootDir(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escaped")

	for _, step := range []Step{
		{Step: "touch", Path: "../escaped"},
		{Step: "createDir", Path: outside},
		{Step: "deletePath", Path: "../.."},
		{Step: "cd", Path: ".."},
	} {
		_, err := run(t, Options{RootDir: root, Steps: []Step{step}})
		if err == nil || !strings.Contains(err.Error(), "outside the run directory") {
			t.Errorf("step %q: got %v, want a confinement error", step.Step, err)
		}
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Error("a step escaped the run directory")
	}
}

func TestCdBackToRootIsAllowed(t *testing.T) {
	// Existing specs cd into a build dir and back out; only going above the
	// root is rejected.
	root := t.TempDir()
	_, err := run(t, Options{
		RootDir: root,
		Steps: []Step{
			{Step: "createDir", Path: ".build"},
			{Step: "cd", Path: ".build"},
			{Step: "cd", Path: ".."},
			{Step: "touch", Path: "at-root"},
		},
	})
	if err != nil {
		t.Fatalf("cd .. back to root should be allowed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "at-root")); err != nil {
		t.Errorf("expected file at root: %v", err)
	}
}

func TestAllowPathEscapeOptsOut(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "subvol")
	_, err := run(t, Options{
		RootDir:         root,
		AllowPathEscape: true,
		Steps:           []Step{{Step: "createDir", Path: target}},
	})
	if err != nil {
		t.Fatalf("AllowPathEscape should permit an absolute path: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("directory not created outside root: %v", err)
	}
}
