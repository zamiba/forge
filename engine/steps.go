package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// builtinSteps returns a fresh handler map for a run. It is rebuilt per run so
// a host overriding a builtin cannot leak that override into other runs.
func builtinSteps() map[string]StepFunc {
	return map[string]StepFunc{
		"cd":               stepCd,
		"fetch":            stepFetch,
		"extract":          stepExtract,
		"copy":             stepCopy,
		"move":             stepMove,
		"make":             stepMake,
		"run":              stepRun,
		"createDir":        stepCreateDir,
		"touch":            stepTouch,
		"deletePath":       stepDeletePath,
		"defineExecutable": stepDefineExecutable,
	}
}

// BuiltinStepNames returns the builtin step types in a stable order.
func BuiltinStepNames() []string {
	names := make([]string, 0, len(builtinSteps()))
	for name := range builtinSteps() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func stepCd(_ context.Context, st *State, step Step) error {
	if step.Path == "" {
		return fmt.Errorf("cd requires a path")
	}
	dir, err := st.Resolve(step.Path)
	if err != nil {
		return err
	}
	st.WorkDir = dir
	return nil
}

func stepFetch(ctx context.Context, st *State, step Step) error {
	url := st.Interp(step.URL)
	if url == "" {
		return fmt.Errorf("fetch requires a url")
	}
	if step.Dest == "" {
		return fmt.Errorf("fetch requires a dest")
	}
	dest, err := st.Resolve(step.Dest)
	if err != nil {
		return err
	}
	return fetch(ctx, st.client, url, dest, func(pct int) {
		st.Emit(Event{Kind: EventProgress, Phase: "fetch", Percent: pct, Step: "fetch", Label: url})
	})
}

func stepExtract(_ context.Context, st *State, step Step) error {
	if step.Src == "" || step.Dest == "" {
		return fmt.Errorf("extract requires src and dest")
	}
	src, err := st.Resolve(step.Src)
	if err != nil {
		return err
	}
	dest, err := st.Resolve(step.Dest)
	if err != nil {
		return err
	}
	return Extract(src, dest)
}

func stepCopy(ctx context.Context, st *State, step Step) error {
	if step.Dest == "" {
		return fmt.Errorf("copy requires a dest")
	}
	var src string
	if step.From != "" {
		resolved, err := st.resolveProvider(ctx, step)
		if err != nil {
			return err
		}
		src = resolved
	} else {
		if step.Src == "" {
			return fmt.Errorf("copy requires a src")
		}
		resolved, err := st.Resolve(step.Src)
		if err != nil {
			return err
		}
		src = resolved
	}
	dest, err := st.Resolve(step.Dest)
	if err != nil {
		return err
	}
	return CopyPath(src, dest)
}

func stepMove(_ context.Context, st *State, step Step) error {
	if step.Src == "" || step.Dest == "" {
		return fmt.Errorf("move requires src and dest")
	}
	src, err := st.Resolve(step.Src)
	if err != nil {
		return err
	}
	dest, err := st.Resolve(step.Dest)
	if err != nil {
		return err
	}
	st.Logf("  src:  %s", src)
	st.Logf("  dest: %s", dest)
	if err := MovePath(src, dest); err != nil {
		return fmt.Errorf("src=%s dest=%s: %w", src, dest, err)
	}
	return nil
}

// stepMake is `run` pinned to make. It predates the run step and stays for
// compatibility with existing specs; it does not consult the allowlist.
func stepMake(ctx context.Context, st *State, step Step) error {
	return runProcess(ctx, st, "make", step)
}

// stepRun executes a declared dependency. The command must appear in the spec's
// dependencies array — that array is the allowlist, so reading it tells you
// every command a spec is able to invoke. Arguments are passed to the process
// directly and are never interpreted by a shell.
//
// A command containing a path separator is a local command: a file inside the
// run directory, typically one an earlier step downloaded, such as a port's own
// installer. It resolves like any other step path — relative to the working
// directory and confined to the run directory — so "../tool" and an absolute
// path are refused here for the same reason they are refused in a copy.
func stepRun(ctx context.Context, st *State, step Step) error {
	cmd := st.Interp(step.Cmd)
	if cmd == "" {
		return fmt.Errorf("run requires a cmd")
	}
	if !st.allowed[cmd] {
		declared := make([]string, 0, len(st.allowed))
		for d := range st.allowed {
			declared = append(declared, d)
		}
		sort.Strings(declared)
		if len(declared) == 0 {
			return fmt.Errorf("run %q: the spec declares no dependencies; add %q to its dependencies array to allow it", cmd, cmd)
		}
		return fmt.Errorf("run %q: not declared in dependencies (declared: %s)", cmd, strings.Join(declared, ", "))
	}
	if IsLocalCommand(cmd) {
		full, err := st.Resolve(cmd)
		if err != nil {
			return err
		}
		if _, err := os.Stat(full); err != nil {
			return fmt.Errorf("run %q: %w", cmd, err)
		}
		cmd = full
	}
	return runProcess(ctx, st, cmd, step)
}

func runProcess(ctx context.Context, st *State, name string, step Step) error {
	args := make([]string, len(step.Args))
	for i, a := range step.Args {
		args[i] = st.Interp(a)
	}
	env := make(map[string]string, len(step.Env))
	for k, v := range step.Env {
		env[k] = st.Interp(v)
	}
	return st.RunCommand(st.Command(ctx, name, args, env))
}

func stepCreateDir(_ context.Context, st *State, step Step) error {
	if step.Path == "" {
		return fmt.Errorf("createDir requires a path")
	}
	dir, err := st.Resolve(step.Path)
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

func stepTouch(_ context.Context, st *State, step Step) error {
	if step.Path == "" {
		return fmt.Errorf("touch requires a path")
	}
	full, err := st.Resolve(step.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	return f.Close()
}

func stepDeletePath(_ context.Context, st *State, step Step) error {
	if step.Path == "" {
		return fmt.Errorf("deletePath requires a path")
	}
	full, err := st.Resolve(step.Path)
	if err != nil {
		return err
	}
	keep, err := st.preservedUnder(full)
	if err != nil {
		return err
	}
	if len(keep) == 0 {
		return os.RemoveAll(full)
	}
	st.Logf("  keeping: %s", strings.Join(keep, ", "))
	return deletePreserving(full, keep)
}

func stepDefineExecutable(_ context.Context, st *State, step Step) error {
	if step.Executable == "" {
		return fmt.Errorf("defineExecutable requires an executable")
	}
	// Recorded relative to RootDir, so the path stays valid if the host later
	// moves the item's directory.
	exePath := st.Interp(step.Executable)
	if runtime.GOOS != "windows" {
		_ = os.Chmod(filepath.Join(st.RootDir, exePath), 0755)
	}
	title := st.Interp(step.Title)
	if title == "" {
		title = filepath.Base(exePath)
	}
	st.AddExecutable(Executable{Path: exePath, Title: title})
	return nil
}
