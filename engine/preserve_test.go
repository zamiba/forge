package engine

import (
	"os"
	"path/filepath"
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

func TestUserDataPathEqualToTheDeletedPathIsAnError(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "install", "saves", "slot1.sav"), "save data")

	_, err := run(t, Options{
		RootDir:       root,
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
