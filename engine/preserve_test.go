package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// write creates a file and every directory above it.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	return err == nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// A delete with nothing to preserve must behave exactly as it did before the
// feature existed.
func TestDeletePathWithoutUserDataPathsRemovesEverything(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "game"), "binary")

	if _, err := run(t, Options{
		RootDir: root,
		Steps:   []Step{{Step: "deletePath", Path: "install"}},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if exists(t, filepath.Join(root, "install")) {
		t.Error("install/ should be gone")
	}
}

func TestDeletePathKeepsUserDataPaths(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "game"), "binary")
	write(t, filepath.Join(root, "install", "game.config"), "fullscreen=1")
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "res", "texture.png"), "asset")

	if _, err := run(t, Options{
		RootDir:       root,
		Teardown:      true,
		PreservePaths: []string{"install/saves", "install/game.config"},
		Steps:         []Step{{Step: "deletePath", Path: "install"}},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := readFile(t, filepath.Join(root, "install", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("save contents = %q", got)
	}
	if got := readFile(t, filepath.Join(root, "install", "game.config")); got != "fullscreen=1" {
		t.Errorf("config contents = %q", got)
	}
	for _, gone := range []string{"game", "res"} {
		if exists(t, filepath.Join(root, "install", gone)) {
			t.Errorf("install/%s should have been deleted", gone)
		}
	}
	if exists(t, filepath.Join(root, tmpPrefix+"install")) {
		t.Error("the scratch directory should have been cleaned up")
	}
}

// The case a glob-and-exclude design gets wrong: the preserved path is nested
// below a directory that the delete would otherwise remove wholesale.
func TestDeletePathKeepsPathsNestedBelowDeletedDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "data", "saves", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "data", "assets", "big.pak"), "assets")

	if _, err := run(t, Options{
		RootDir:       root,
		Teardown:      true,
		PreservePaths: []string{"install/data/saves"},
		Steps:         []Step{{Step: "deletePath", Path: "install"}},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := readFile(t, filepath.Join(root, "install", "data", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("nested save contents = %q", got)
	}
	if exists(t, filepath.Join(root, "install", "data", "assets")) {
		t.Error("the sibling of a preserved path should still be deleted")
	}
}

// Interrupting the operation after the target has been set aside must not lose
// data: re-running with the same list finishes the job.
func TestDeletePreservingResumesAfterInterruption(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "install")
	tmp := filepath.Join(root, tmpPrefix+"install")

	// The state a crash leaves between the rename and the restore: everything
	// is in the scratch directory and the target does not exist.
	write(t, filepath.Join(tmp, "game"), "binary")
	write(t, filepath.Join(tmp, "saves", "slot1.sav"), "save data")

	if err := deletePreserving(target, []string{"saves"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := readFile(t, filepath.Join(target, "saves", "slot1.sav")); got != "save data" {
		t.Errorf("save contents after resume = %q", got)
	}
	if exists(t, tmp) {
		t.Error("scratch directory should be gone after a resumed run")
	}
	if exists(t, filepath.Join(target, "game")) {
		t.Error("the resumed run should still have deleted the program files")
	}
}

// A crash partway through the restore leaves preserved paths split across both
// trees. Finishing must find them wherever they are.
func TestDeletePreservingResumesWithPartiallyRestoredPaths(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "install")
	tmp := filepath.Join(root, tmpPrefix+"install")

	write(t, filepath.Join(tmp, "game"), "binary")
	write(t, filepath.Join(tmp, "config.ini"), "still here")
	write(t, filepath.Join(target, "saves", "slot1.sav"), "already moved")

	if err := deletePreserving(target, []string{"saves", "config.ini"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := readFile(t, filepath.Join(target, "saves", "slot1.sav")); got != "already moved" {
		t.Errorf("already-restored save = %q", got)
	}
	if got := readFile(t, filepath.Join(target, "config.ini")); got != "still here" {
		t.Errorf("remaining config = %q", got)
	}
	if exists(t, tmp) {
		t.Error("scratch directory should be gone")
	}
}

// Running the whole operation twice must be a no-op the second time rather than
// an error.
func TestDeletePreservingIsIdempotent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "install")
	write(t, filepath.Join(target, "game"), "binary")
	write(t, filepath.Join(target, "saves", "slot1.sav"), "save data")

	for i := 0; i < 2; i++ {
		if err := deletePreserving(target, []string{"saves"}); err != nil {
			t.Fatalf("pass %d: %v", i+1, err)
		}
	}
	if got := readFile(t, filepath.Join(target, "saves", "slot1.sav")); got != "save data" {
		t.Errorf("save contents = %q", got)
	}
}

// A port that has never been launched has written no saves. That is not an
// error, and it must not stop the rest of the delete.
func TestPreservedPathThatWasNeverCreatedIsNotAnError(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "game"), "binary")

	if _, err := run(t, Options{
		RootDir:       root,
		Teardown:      true,
		PreservePaths: []string{"install/saves"},
		Steps:         []Step{{Step: "deletePath", Path: "install"}},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if exists(t, filepath.Join(root, "install", "game")) {
		t.Error("program files should still have been deleted")
	}
}

// Preserved paths belong to the whole run, so an unrelated delete elsewhere in
// the tree must not consult them.
func TestUserDataPathsOnlyAffectDeletesThatContainThem(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")
	write(t, filepath.Join(root, ".build", "obj.o"), "artefact")

	if _, err := run(t, Options{
		RootDir:       root,
		Teardown:      true,
		PreservePaths: []string{"install/saves"},
		Steps:         []Step{{Step: "deletePath", Path: ".build"}},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if exists(t, filepath.Join(root, ".build")) {
		t.Error(".build should have been removed outright")
	}
	if exists(t, filepath.Join(root, tmpPrefix+".build")) {
		t.Error("an unrelated delete should not create a scratch directory")
	}
	if !exists(t, filepath.Join(root, "install", "saves", "slot1.sav")) {
		t.Error("the preserved path was not in this delete's tree and should be untouched")
	}
}

// They resolve against RootDir rather than the working directory, so a `cd`
// earlier in the sequence cannot change what they refer to.
func TestUserDataPathsResolveAgainstRootNotWorkDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "game"), "binary")

	if _, err := run(t, Options{
		RootDir:       root,
		Teardown:      true,
		PreservePaths: []string{"install/saves"},
		Steps: []Step{
			{Step: "cd", Path: "install"},
			{Step: "cd", Path: ".."},
			{Step: "deletePath", Path: "install"},
		},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !exists(t, filepath.Join(root, "install", "saves", "slot1.sav")) {
		t.Error("preserved path should have survived")
	}
}

func TestUserDataPathOutsideRootIsRejectedBeforeAnyStepRuns(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "ran")

	_, err := run(t, Options{
		RootDir:       root,
		PreservePaths: []string{"../elsewhere"},
		Steps:         []Step{{Step: "touch", Path: "ran"}},
	})
	if err == nil {
		t.Fatal("expected a userDataPaths error")
	}
	if !strings.Contains(err.Error(), "userDataPaths") {
		t.Errorf("error should name the field: %v", err)
	}
	if exists(t, marker) {
		t.Error("no step should have run")
	}
}

// Only a teardown can hit this: during an install the data is out of the tree
// and deletePath has nothing to spare, so there is no contradiction to report.
func TestUserDataPathEqualToTheDeletedPathIsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")

	_, err := run(t, Options{
		RootDir:       root,
		Teardown:      true,
		PreservePaths: []string{"install"},
		Steps:         []Step{{Step: "deletePath", Path: "install"}},
	})
	if err == nil {
		t.Fatal("expected an error: preserving the deleted path means deleting nothing")
	}
	if !strings.Contains(err.Error(), "being deleted") {
		t.Errorf("unhelpful error: %v", err)
	}
	if !exists(t, filepath.Join(root, "install", "saves", "slot1.sav")) {
		t.Error("nothing should have been deleted")
	}
}

func TestUserDataPathsInterpolateArgs(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "us", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "game"), "binary")

	if _, err := run(t, Options{
		RootDir:       root,
		Args:          map[string]string{"region": "us"},
		Teardown:      true,
		PreservePaths: []string{"install/${args.region}"},
		Steps:         []Step{{Step: "deletePath", Path: "install"}},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !exists(t, filepath.Join(root, "install", "us", "slot1.sav")) {
		t.Error("interpolated preserve path should have been honoured")
	}
}

func TestUserDataPathsReachEveryBuildFromTheFile(t *testing.T) {
	sf, err := ParseSpecFile([]byte(`{
	  "userDataPaths": ["install/saves", "install/data/saves"],
	  "builds": [
	    { "versions": ["1.0"], "steps": [] },
	    { "versions": ["2.0"], "steps": [] }
	  ]
	}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for i := range sf.Specs {
		if got := sf.Specs[i].UserDataPaths; len(got) != 2 {
			t.Errorf("build %d got %v, want both file-level paths", i, got)
		}
	}
}

// A program that moved its save directory between versions lists both places
// rather than splitting the declaration: the one that is not there is skipped.
func TestUserDataPathsAreRejectedOnABuild(t *testing.T) {
	_, err := ParseSpecFile([]byte(`{
	  "builds": [{ "versions": ["1.0"], "userDataPaths": ["install/saves"], "steps": [] }]
	}`))
	if err == nil {
		t.Fatal("a build declaring userDataPaths should be rejected")
	}
	if !strings.Contains(err.Error(), "belongs to the file") {
		t.Errorf("the error should say where it goes instead: %v", err)
	}
}

func TestUndeclaredArgsCoversUserDataPaths(t *testing.T) {
	spec := &Spec{UserDataPaths: []string{"install/${args.profile}"}}
	if got := UndeclaredArgs(spec); len(got) != 1 || got[0] != "args.profile" {
		t.Errorf("UndeclaredArgs = %v, want [args.profile]", got)
	}
}

func TestSetAsideAndPutBackRoundTrip(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, ".tmp-preserve")
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "game.config"), "user edited")

	paths := []string{"install/saves", "install/game.config"}
	if err := SetAside(root, scratch, paths); err != nil {
		t.Fatalf("SetAside: %v", err)
	}
	if exists(t, filepath.Join(root, "install", "saves")) {
		t.Error("saves should have been moved out of the tree")
	}

	// Stand in for a build that ships its own copy of a file the user edited.
	write(t, filepath.Join(root, "install", "game.config"), "shipped default")

	if err := PutBack(scratch, root, paths); err != nil {
		t.Fatalf("PutBack: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "install", "game.config")); got != "user edited" {
		t.Errorf("the preserved copy should win, got %q", got)
	}
	if got := readFile(t, filepath.Join(root, "install", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("save contents = %q", got)
	}
	if exists(t, scratch) {
		t.Error("scratch directory should be gone")
	}
}

// A crash between the two halves leaves data in the scratch directory. The next
// attempt must not lose it: SetAside finds nothing to move, PutBack returns it.
func TestSetAsideDoesNotClobberAnInterruptedAside(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, ".tmp-preserve")
	write(t, filepath.Join(scratch, "install", "saves", "slot1.sav"), "save data")

	paths := []string{"install/saves"}
	if err := SetAside(root, scratch, paths); err != nil {
		t.Fatalf("SetAside: %v", err)
	}
	if err := PutBack(scratch, root, paths); err != nil {
		t.Fatalf("PutBack: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "install", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("data from the interrupted run = %q", got)
	}
}

func TestSetAsideSkipsPathsThatDoNotExist(t *testing.T) {
	root := t.TempDir()
	scratch := filepath.Join(root, ".tmp-preserve")
	write(t, filepath.Join(root, "install", "game"), "binary")

	paths := []string{"install/saves"}
	if err := SetAside(root, scratch, paths); err != nil {
		t.Fatalf("SetAside: %v", err)
	}
	if err := PutBack(scratch, root, paths); err != nil {
		t.Fatalf("PutBack: %v", err)
	}
	if exists(t, scratch) {
		t.Error("scratch directory should be gone")
	}
}

// An install runs over the previous one. A build shipping its own copy of a
// file the user has edited is not a delete, so deletePath never sees it — the
// engine has to move the data out of the way itself.
func TestRunProtectsUserDataFromABuildWritingOverIt(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "game.config"), "user edited")

	spec := specFromJSON(t, `{
	  "userDataPaths": ["install/saves", "install/game.config"],
	  "builds": [{"versions": ["1.0"], "steps": []}]
	}`)
	opts := spec.BuildOptions("Linux", "1.0", nil, []string{"1.0"})
	opts.RootDir = root
	opts.Steps = []Step{
		{Step: "touch", Path: "install/game"},
		// Stands in for a build clearing its config directory and shipping a
		// fresh default over the user's copy.
		{Step: "deletePath", Path: "install/game.config"},
		{Step: "touch", Path: "install/game.config"},
	}

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "install", "game.config")); got != "user edited" {
		t.Errorf("the user's copy should win, got %q", got)
	}
	if got := readFile(t, filepath.Join(root, "install", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("saves = %q", got)
	}
	if exists(t, filepath.Join(root, userDataScratch)) {
		t.Error("the scratch directory should be gone")
	}
}

// A build that dies must not take the data with it.
func TestRunRestoresUserDataWhenTheBuildFails(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")

	_, err := run(t, Options{
		RootDir:       root,
		PreservePaths: []string{"install/saves"},
		Steps:         []Step{{Step: "deletePath", Path: "../escape"}},
	})
	if err == nil {
		t.Fatal("expected the run to fail")
	}
	if got := readFile(t, filepath.Join(root, "install", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("saves after a failed build = %q", got)
	}
	if exists(t, filepath.Join(root, userDataScratch)) {
		t.Error("the scratch directory should be gone")
	}
}

// A teardown is protected by deletePath alone. Setting the data aside as well
// would leave every uninstall with an install directory holding just the saves.
func TestTeardownDoesNotSetUserDataAside(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "game"), "binary")

	spec := specFromJSON(t, `{
	  "userDataPaths": ["install/saves"],
	  "uninstallSteps": [{"step": "deletePath", "path": "install"}],
	  "builds": [{"versions": ["1.0"], "steps": []}]
	}`)
	opts := spec.TeardownOptions("Linux", "1.0", nil, []string{"1.0"})
	opts.RootDir = root
	if !opts.Teardown || !opts.SkipDependencyCheck {
		t.Fatal("TeardownOptions should set Teardown and skip the dependency check")
	}

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "install", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("saves = %q", got)
	}
	if exists(t, filepath.Join(root, "install", "game")) {
		t.Error("the program should have been removed")
	}
}

// Assembling Options by hand is one chance to forget per field, and the cost of
// forgetting the last one is a deleted save file.
func TestBuildOptionsCarriesEverySpecDerivedField(t *testing.T) {
	spec := specFromJSON(t, `{
	  "dependencies": ["make"],
	  "userDataPaths": ["install/saves"],
	  "builds": [{
	    "versions": {"1.0": {"tag": "v1.0"}},
	    "targetPlatforms": {"Linux": {"slug": "linux"}},
	    "steps": [{"step": "touch", "path": "x"}]
	  }]
	}`)
	opts := spec.BuildOptions("Linux", "1.0", map[string]string{"region": "us"}, []string{"1.0"})

	for name, ok := range map[string]bool{
		"Steps":         len(opts.Steps) == 1,
		"Dependencies":  len(opts.Dependencies) == 1,
		"Args":          opts.Args["region"] == "us",
		"PlatformVars":  opts.PlatformVars["slug"] == "linux",
		"VersionVars":   opts.VersionVars["tag"] == "v1.0",
		"PreservePaths": len(opts.PreservePaths) == 1,
		"Platform":      opts.Platform == "Linux",
		"Version":       opts.Version == "1.0",
		"VersionOrder":  len(opts.VersionOrder) == 1,
		"Teardown":      !opts.Teardown,
	} {
		if !ok {
			t.Errorf("BuildOptions did not carry %s", name)
		}
	}
}

// An entry of userDataPaths is a string for a path inside the tree, or an
// object naming a location outside it. The engine protects the former and
// only carries the latter, for the host.
func TestUserDataPathsTakeEntriesOutsideTheTree(t *testing.T) {
	sf, err := ParseSpecFile([]byte(`{
	  "userDataPaths": [
	    "install/saves",
	    { "locationType": "linuxData", "path": "melee-pc" },
	    { "locationType": "windowsRoaming", "path": "melee-pc" },
	    "install/game.config"
	  ],
	  "builds": [{ "versions": ["1.0"], "steps": [] }]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	spec := sf.Specs[0]
	if got := strings.Join(spec.UserDataPaths, ","); got != "install/saves,install/game.config" {
		t.Errorf("in-tree paths = %q", got)
	}
	if len(spec.UserData) != 4 || !spec.UserData[1].Outside() || spec.UserData[1].LocationType != "linuxData" || spec.UserData[1].Path != "melee-pc" {
		t.Errorf("UserData = %+v", spec.UserData)
	}
	if spec.UserData[0].Outside() || spec.UserData[0].Path != "install/saves" {
		t.Errorf("a string entry is an in-tree path: %+v", spec.UserData[0])
	}
	// The build's protection is unchanged by the entries outside the tree.
	opts := spec.BuildOptions("Linux", "1.0", nil, nil)
	if got := strings.Join(opts.PreservePaths, ","); got != "install/saves,install/game.config" {
		t.Errorf("PreservePaths = %q", got)
	}
	// And the declaration round-trips in the form it was written.
	out, err := json.Marshal(spec.UserData)
	if err != nil {
		t.Fatal(err)
	}
	if want := `["install/saves",{"locationType":"linuxData","path":"melee-pc"},{"locationType":"windowsRoaming","path":"melee-pc"},"install/game.config"]`; string(out) != want {
		t.Errorf("marshal = %s", out)
	}
}

func TestUserDataPathsRejectMalformedEntriesOutsideTheTree(t *testing.T) {
	for name, entry := range map[string]string{
		"no location type": `{ "path": "saves" }`,
		"unknown type":     `{ "locationType": "linuxLocal", "path": "saves" }`,
		"no path":          `{ "locationType": "linuxData" }`,
		"interpolation":    `{ "locationType": "linuxData", "path": "${args.name}" }`,
		"absolute":         `{ "locationType": "linuxData", "path": "/etc" }`,
		"escapes":          `{ "locationType": "linuxData", "path": "../.ssh" }`,
		"the folder":       `{ "locationType": "linuxData", "path": "." }`,
		"not a path":       `42`,
	} {
		_, err := ParseSpecFile([]byte(`{ "userDataPaths": [` + entry + `], "builds": [{ "versions": ["1.0"], "steps": [] }] }`))
		if err == nil {
			t.Errorf("%s: %s should be rejected", name, entry)
		}
	}
}

// A location resolves on the platform it belongs to and says so on any
// other, so a host skips the entries that are not for this machine.
func TestUserDataPathLocationFollowsThePlatform(t *testing.T) {
	linux := UserDataPath{LocationType: "linuxData", Path: "melee-pc"}
	windows := UserDataPath{LocationType: "windowsRoaming", Path: "melee-pc"}
	switch runtime.GOOS {
	case "linux":
		t.Setenv("XDG_DATA_HOME", "/tmp/xdg")
		if got, err := linux.Location(); err != nil || got != "/tmp/xdg/melee-pc" {
			t.Errorf("linuxData = %q, %v", got, err)
		}
		if _, err := windows.Location(); !errors.Is(err, ErrOtherPlatform) {
			t.Errorf("windowsRoaming on linux: %v", err)
		}
	case "windows":
		t.Setenv("APPDATA", `C:\Users\x\AppData\Roaming`)
		if got, err := windows.Location(); err != nil || got != `C:\Users\x\AppData\Roaming\melee-pc` {
			t.Errorf("windowsRoaming = %q, %v", got, err)
		}
		if _, err := linux.Location(); !errors.Is(err, ErrOtherPlatform) {
			t.Errorf("linuxData on windows: %v", err)
		}
	}
	if got := strings.Join(LocationTypes(), " "); !strings.Contains(got, "linuxData") || !strings.Contains(got, "windowsSavedGames") {
		t.Errorf("LocationTypes = %q", got)
	}
}

// runDir is the object spelling of an in-tree path. It has to behave exactly as
// the bare string does — same list, same protection — because that is the whole
// claim being made by having two spellings.
func TestUserDataPathsTakeARunDirEntry(t *testing.T) {
	sf, err := ParseSpecFile([]byte(`{
	  "userDataPaths": [
	    { "locationType": "runDir", "path": "install/saves" },
	    "install/game.config",
	    { "locationType": "linuxData", "path": "melee-pc" }
	  ],
	  "builds": [{ "versions": ["1.0"], "steps": [] }]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	spec := sf.Specs[0]

	// The runDir entry sits in the in-tree list beside the string, in order.
	if got := strings.Join(spec.UserDataPaths, ","); got != "install/saves,install/game.config" {
		t.Errorf("in-tree paths = %q", got)
	}
	if spec.UserData[0].Outside() {
		t.Errorf("a runDir entry is not outside the tree: %+v", spec.UserData[0])
	}
	if !spec.UserData[2].Outside() {
		t.Errorf("a per-user folder entry is: %+v", spec.UserData[2])
	}
	if got := strings.Join(spec.BuildOptions("Linux", "1.0", nil, nil).PreservePaths, ","); got != "install/saves,install/game.config" {
		t.Errorf("PreservePaths = %q", got)
	}

	// Each entry round-trips in the spelling its author used, so reading a file
	// and writing it back does not quietly convert either one into the other.
	out, err := json.Marshal(spec.UserData)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"locationType":"runDir","path":"install/saves"},"install/game.config",{"locationType":"linuxData","path":"melee-pc"}]`
	if string(out) != want {
		t.Errorf("marshal = %s", out)
	}
}

// The protection is the point, so assert it against a build that overwrites the
// path rather than only against the list it lands in.
func TestRunProtectsARunDirEntry(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")

	spec := specFromJSON(t, `{
	  "userDataPaths": [{ "locationType": "runDir", "path": "install/saves" }],
	  "builds": [{"versions": ["1.0"], "steps": []}]
	}`)
	opts := spec.BuildOptions("Linux", "1.0", nil, []string{"1.0"})
	opts.RootDir = root
	opts.Steps = []Step{
		{Step: "deletePath", Path: "install/saves"},
		{Step: "touch", Path: "install/saves"},
	}

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "install", "saves", "slot1.sav")); got != "save data" {
		t.Errorf("the player's save should have survived, got %q", got)
	}
}

// An in-tree path is a step path, so it keeps the variables the string form has.
// A path outside the tree does not, since the engine resolves the location and
// runs nothing there.
func TestRunDirPathInterpolatesButAnOutsideOneDoesNot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "us", "slot1.sav"), "save data")
	write(t, filepath.Join(root, "install", "game"), "binary")

	spec := specFromJSON(t, `{
	  "userDataPaths": [{ "locationType": "runDir", "path": "install/${args.region}" }],
	  "builds": [{"versions": ["1.0"], "steps": []}]
	}`)
	opts := spec.BuildOptions("Linux", "1.0", map[string]string{"region": "us"}, []string{"1.0"})
	opts.RootDir = root
	opts.Teardown = true
	opts.Steps = []Step{{Step: "deletePath", Path: "install"}}

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !exists(t, filepath.Join(root, "install", "us", "slot1.sav")) {
		t.Error("an interpolated runDir path should have been honoured")
	}

	if _, err := ParseSpecFile([]byte(`{
	  "userDataPaths": [{ "locationType": "linuxData", "path": "${args.region}" }],
	  "builds": [{ "versions": ["1.0"], "steps": [] }]
	}`)); err == nil {
		t.Error("a path outside the tree is literal and should be rejected")
	}
}

func TestUserDataPathsRejectMalformedRunDirEntries(t *testing.T) {
	for name, entry := range map[string]string{
		"no path":     `{ "locationType": "runDir" }`,
		"absolute":    `{ "locationType": "runDir", "path": "/etc" }`,
		"escapes":     `{ "locationType": "runDir", "path": "../elsewhere" }`,
		"the run dir": `{ "locationType": "runDir", "path": "." }`,
		"misspelled":  `{ "locationType": "rundir", "path": "install/saves" }`,
	} {
		_, err := ParseSpecFile([]byte(`{ "userDataPaths": [` + entry + `], "builds": [{ "versions": ["1.0"], "steps": [] }] }`))
		if err == nil {
			t.Errorf("%s: %s should be rejected", name, entry)
		}
	}
}

// Location has nothing to resolve for an in-tree entry, in either spelling: the
// path is relative to the run directory the caller already holds.
func TestLocationOnAnInTreeEntry(t *testing.T) {
	for _, u := range []UserDataPath{
		{LocationType: LocationRunDir, Path: "install/saves"},
		{Path: "install/saves"},
	} {
		if _, err := u.Location(); !errors.Is(err, ErrInsideRunDir) {
			t.Errorf("%+v: Location error = %v, want ErrInsideRunDir", u, err)
		}
	}
}
