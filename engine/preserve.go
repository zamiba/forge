package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// tmpPrefix names the directory a preserving delete works through. It sits
// beside the target, inside RootDir, so the whole operation stays within the
// confinement boundary and every rename is on one filesystem.
const tmpPrefix = ".tmp-"

// userDataScratch is where an install's user data waits out the build. It sits
// at the root of the run, dot-prefixed, and is removed when the data goes back.
const userDataScratch = ".tmp-userdata"

// preservedUnder returns the run's preserved paths that live inside target, as
// paths relative to it. A preserved path elsewhere in the tree is not this
// delete's concern; one that *is* the target is a contradiction and an error,
// since honouring it would mean deleting nothing.
func (st *State) preservedUnder(target string) ([]string, error) {
	var out []string
	for _, p := range st.preservePaths {
		full, err := st.resolveAt(st.RootDir, p)
		if err != nil {
			return nil, fmt.Errorf("userDataPaths: %w", err)
		}
		rel, err := filepath.Rel(target, full)
		if err != nil {
			continue
		}
		if rel == "." {
			return nil, fmt.Errorf("userDataPaths: %q is the path being deleted", p)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, rel)
	}
	return out, nil
}

// deletePreserving removes target while keeping the listed paths, which are
// relative to it.
//
// The order matters more than it looks. Everything moves rather than copies, so
// a large save directory costs nothing; the target is set aside whole in one
// rename before anything is destroyed; and the irreversible delete happens last,
// once every preserved path already sits in its final home.
//
// It is also resumable. Once the target has been set aside, restorePreserved is
// idempotent: interrupt it anywhere and running it again with the same list
// finishes the job, because each preserved path is in exactly one of the two
// trees and the leftovers are all in the one being thrown away.
func deletePreserving(target string, keep []string) error {
	tmp := filepath.Join(filepath.Dir(target), tmpPrefix+filepath.Base(target))

	switch _, err := os.Lstat(tmp); {
	case err == nil:
		// An earlier attempt was interrupted. Finish that one rather than
		// starting over: between the two trees nothing has been lost yet, but
		// setting the partial target aside a second time would bury it.
		return restorePreserved(tmp, target, keep)
	case !os.IsNotExist(err):
		return err
	}

	if _, err := os.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to delete, as with a plain RemoveAll
		}
		return err
	}
	if err := os.Rename(target, tmp); err != nil {
		return fmt.Errorf("set %s aside: %w", target, err)
	}
	return restorePreserved(tmp, target, keep)
}

// restorePreserved rebuilds target from the paths kept out of tmp, then removes
// what is left. Safe to re-run: a preserved path already moved is simply absent
// from tmp the second time around.
func restorePreserved(tmp, target string, keep []string) error {
	mode := os.FileMode(0755)
	if info, err := os.Stat(tmp); err == nil && info.IsDir() {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(target, mode); err != nil {
		return err
	}
	return PutBack(tmp, target, keep)
}

// SetAside moves paths — each relative to root — into scratch, creating it.
// Paths that do not exist are skipped, so a program that has never written its
// save file is not an error.
//
// It is the first half of running an operation over a tree that holds data the
// operation must not disturb: set the data aside, do the work, PutBack. A
// deletePath honours preservePaths on its own, but a build that rewrites a file
// the user has since edited needs the data physically out of the way.
//
// The pair survives interruption. If scratch already holds a path because an
// earlier attempt was killed between the two halves, the source is simply gone
// and the entry is skipped, so the next PutBack still returns it.
func SetAside(root, scratch string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if err := os.MkdirAll(scratch, 0755); err != nil {
		return err
	}
	return movePaths(root, scratch, paths)
}

// PutBack moves paths from scratch back under root, replacing anything at each
// destination, and removes scratch. The preserved copy wins: that is the point
// of having set it aside.
func PutBack(scratch, root string, paths []string) error {
	if err := movePaths(scratch, root, paths); err != nil {
		return err
	}
	return os.RemoveAll(scratch)
}

// movePaths moves each rel from one tree to the other, creating parents and
// replacing whatever is already at the destination. A rel that is missing from
// the source is skipped rather than reported: across both callers that means
// either "never created" or "a previous attempt already moved it", and neither
// is a failure.
func movePaths(src, dst string, rels []string) error {
	for _, rel := range rels {
		from := filepath.Join(src, rel)
		if _, err := os.Lstat(from); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		to := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
			return err
		}
		// Whatever is at the destination is the copy being superseded — a
		// partial write from an interrupted run, or a default the build just
		// shipped over the user's edited one.
		if err := os.RemoveAll(to); err != nil {
			return err
		}
		if err := MovePath(from, to); err != nil {
			return fmt.Errorf("move %s: %w", rel, err)
		}
	}
	return nil
}
