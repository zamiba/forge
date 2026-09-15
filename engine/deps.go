package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// msys2Dep is a pseudo-dependency: rather than naming a command on PATH it asks
// for an MSYS2 installation, whose bin directories are prepended to PATH for
// every subprocess the run starts.
const msys2Dep = "msys2"

// IsLocalCommand reports whether a dependency names a file inside the run
// directory rather than a command on PATH. The distinction is the presence of a
// path separator: "install/launcher" is a file the steps produce, "chmod" is a
// command the system provides. A bare name is never looked up in the run
// directory, so an archive that happens to contain a file called "make" cannot
// shadow the real one for a spec that declared it.
func IsLocalCommand(name string) bool {
	return strings.ContainsAny(name, `/\`)
}

// CheckDependencies verifies that every declared dependency is present, and
// returns a single error naming all the missing ones. Local commands are not
// checked: they do not exist until the steps that produce them have run, and
// the run step resolves them — and fails if they are missing — at the moment
// they are invoked.
func CheckDependencies(deps []string) error {
	var missing []string
	for _, dep := range deps {
		if IsLocalCommand(dep) {
			continue
		}
		if dep == msys2Dep {
			if runtime.GOOS == "windows" && FindMSYS2() == "" {
				missing = append(missing, "msys2 (install from https://www.msys2.org)")
			}
			continue
		}
		if _, err := exec.LookPath(dep); err != nil {
			missing = append(missing, dep)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing build dependencies: %s", strings.Join(missing, ", "))
	}
	return nil
}

// FindMSYS2 returns the root of the MSYS2 installation on Windows, or "" if
// none was found in the usual locations.
func FindMSYS2() string {
	candidates := []string{
		`C:\msys64`,
		`C:\msys2`,
		filepath.Join(os.Getenv("USERPROFILE"), "msys64"),
		filepath.Join(os.Getenv("USERPROFILE"), "msys2"),
	}
	for _, root := range candidates {
		if info, err := os.Stat(filepath.Join(root, "usr", "bin")); err == nil && info.IsDir() {
			return root
		}
	}
	return ""
}

// prependMSYS2Path returns env with the MSYS2 bin directories prepended to PATH.
func prependMSYS2Path(env []string, msys2Root string) []string {
	extra := strings.Join([]string{
		filepath.Join(msys2Root, "mingw64", "bin"),
		filepath.Join(msys2Root, "usr", "local", "bin"),
		filepath.Join(msys2Root, "usr", "bin"),
	}, string(os.PathListSeparator))
	for i, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") {
			env[i] = e[:5] + extra + string(os.PathListSeparator) + e[5:]
			return env
		}
	}
	return append(env, "PATH="+extra)
}

// mergeEnv returns the current environment with extra appended on top.
func mergeEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
