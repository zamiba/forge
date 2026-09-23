package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// LocationRunDir is the location type for a path inside the run directory —
// the program's own folder, the place a build writes to. It is the one location
// type that is not a per-user folder and not tied to an operating system, and
// the only one the engine protects rather than merely carries.
const LocationRunDir = "runDir"

// UserDataPath is one entry of a file's userDataPaths. Every entry is an object
// naming where the data lives and a path beneath it:
//
//	"userDataPaths": [
//	  { "locationType": "runDir",    "path": "install/saves" },
//	  { "locationType": "linuxData", "path": "melee-pc" }
//	]
//
// runDir is inside the tree, and those entries are the ones the engine protects:
// spared by deletePath, set aside for the duration of an install. Every other
// location type names one of the per-user folders an operating system gives a
// program for its own files, by a name that says which. Those the engine parses,
// keeps in Spec.UserData and resolves on request, and does nothing else with:
// what happens at that place is the host's. Such an entry is for one platform,
// the one its type names, so a program writing to a different folder on each
// declares one entry per platform.
//
// A bare string is the deprecated spelling of a runDir entry and still reads:
//
//	"userDataPaths": ["install/saves"]
//
// A runDir path is interpolated the way a step's path is, so it may hold
// ${args.x} and the other run variables. A path outside the tree is literal,
// because the engine resolves the location and never runs anything there.
type UserDataPath struct {
	LocationType string `json:"locationType,omitempty"`
	Path         string `json:"path"`
}

// Outside reports whether the entry names a place outside the tree. A runDir
// entry does not, and neither does a bare string, so both take the same path
// through the engine and get the same protection — which is the point of the
// two spellings being the same thing.
func (u UserDataPath) Outside() bool {
	return u.LocationType != "" && u.LocationType != LocationRunDir
}

func (u *UserDataPath) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*u = UserDataPath{Path: s}
		return nil
	}
	type plain UserDataPath
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("userDataPaths: an entry is a string or an object with locationType and path: %w", err)
	}
	if p.LocationType == "" {
		return fmt.Errorf("userDataPaths: an object entry needs a locationType; a path inside the run directory is %q", LocationRunDir)
	}
	if _, known := locationTypes[p.LocationType]; !known && p.LocationType != LocationRunDir {
		return fmt.Errorf("userDataPaths: %q is not a location type; they are %s and %s",
			p.LocationType, LocationRunDir, strings.Join(LocationTypes(), ", "))
	}
	if p.Path == "" {
		return fmt.Errorf("userDataPaths: the %s entry has no path", p.LocationType)
	}
	// Only outside the tree: there the engine resolves the location and runs
	// nothing, so there is nothing to interpolate against. A runDir path is a
	// step path like any other and keeps the variables one can use.
	if p.LocationType != LocationRunDir && strings.Contains(p.Path, "${") {
		return fmt.Errorf("userDataPaths: a %s entry is literal and is not interpolated: %s", p.LocationType, p.Path)
	}
	// Beneath the location, never the location itself or anything above it:
	// the spec names a folder of the program's, not the user's whole home.
	clean := path.Clean(strings.ReplaceAll(p.Path, "\\", "/"))
	if path.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("userDataPaths: %q does not stay beneath %s", p.Path, p.LocationType)
	}
	*u = UserDataPath(p)
	return nil
}

func (u UserDataPath) MarshalJSON() ([]byte, error) {
	// Keyed on the type being absent rather than on Outside(), so a runDir entry
	// written as an object comes back as one instead of being rewritten into the
	// deprecated string form.
	if u.LocationType == "" {
		return json.Marshal(u.Path)
	}
	type plain UserDataPath
	return json.Marshal(plain(u))
}

// ErrOtherPlatform is returned by Location for an entry whose location type
// belongs to another operating system. It is not a fault in the spec: the
// entry is simply not for this machine.
var ErrOtherPlatform = errors.New("the location type is for another platform")

// ErrInsideRunDir is returned by Location for an entry that is inside the run
// directory — a runDir entry, or the bare string that means the same. There is
// no per-user folder to resolve: the path is relative to the run directory the
// caller already has.
var ErrInsideRunDir = errors.New("the entry is inside the run directory")

// Location resolves the entry's location type on this machine and returns
// the place the entry names beneath it: the folder the type stands for, with
// the path joined on in the platform's form. The path was checked at parse
// time to stay beneath the folder, so the result is always inside it.
func (u UserDataPath) Location() (string, error) {
	if !u.Outside() {
		return "", ErrInsideRunDir
	}
	goos, known := locationTypes[u.LocationType]
	if !known {
		return "", fmt.Errorf("userDataPaths: %q is not a location type", u.LocationType)
	}
	if goos != runtime.GOOS {
		return "", ErrOtherPlatform
	}
	dir, err := resolveLocationType(u.LocationType)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.FromSlash(u.Path)), nil
}

// locationTypes names the per-user folders a spec may put user data under,
// and the operating system each belongs to. The names say the folder the
// way its platform's programs do, so an author writes what a port's own
// documentation says rather than translating it: "%APPDATA%" is
// windowsRoaming, "~/.local/share" is linuxData.
var locationTypes = map[string]string{
	"linuxConfig":             "linux",   // $XDG_CONFIG_HOME, else ~/.config
	"linuxData":               "linux",   // $XDG_DATA_HOME, else ~/.local/share
	"windowsRoaming":          "windows", // %APPDATA%
	"windowsLocal":            "windows", // %LOCALAPPDATA%
	"windowsDocuments":        "windows", // the Documents known folder
	"windowsSavedGames":       "windows", // the Saved Games known folder
	"macosApplicationSupport": "darwin",  // ~/Library/Application Support
}

// LocationTypes lists the location types a spec may name, sorted.
func LocationTypes() []string {
	names := make([]string, 0, len(locationTypes))
	for name := range locationTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
