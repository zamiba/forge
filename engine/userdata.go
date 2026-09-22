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

// UserDataPath is one entry of a file's userDataPaths. Written as a string it
// is a path inside the run's root, the form the engine protects: spared by
// deletePath, set aside during an install. Written as an object it names a
// place outside the tree — one of the per-user folders an operating system
// gives a program for its own files, by a name that says which — and a path
// beneath it:
//
//	"userDataPaths": ["install/saves", { "locationType": "linuxData", "path": "melee-pc" }]
//
// The engine parses the object form and keeps it in Spec.UserData, resolves
// the location on request, and does nothing else with it: what happens at
// that place is the host's. An entry is for one platform, the one its
// location type names; a program that writes to a different folder on each
// platform declares one entry per platform. The path is literal — no ${name}
// interpolation.
type UserDataPath struct {
	LocationType string `json:"locationType,omitempty"`
	Path         string `json:"path"`
}

// Outside reports whether the entry names a place outside the tree.
func (u UserDataPath) Outside() bool { return u.LocationType != "" }

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
		return fmt.Errorf("userDataPaths: an object entry needs a locationType; a path inside the tree is written as a plain string")
	}
	if _, known := locationTypes[p.LocationType]; !known {
		return fmt.Errorf("userDataPaths: %q is not a location type; they are %s", p.LocationType, strings.Join(LocationTypes(), ", "))
	}
	if p.Path == "" {
		return fmt.Errorf("userDataPaths: the %s entry has no path", p.LocationType)
	}
	if strings.Contains(p.Path, "${") {
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
	if !u.Outside() {
		return json.Marshal(u.Path)
	}
	type plain UserDataPath
	return json.Marshal(plain(u))
}

// ErrOtherPlatform is returned by Location for an entry whose location type
// belongs to another operating system. It is not a fault in the spec: the
// entry is simply not for this machine.
var ErrOtherPlatform = errors.New("the location type is for another platform")

// Location resolves the entry's location type on this machine and returns
// the place the entry names beneath it: the folder the type stands for, with
// the path joined on in the platform's form. The path was checked at parse
// time to stay beneath the folder, so the result is always inside it.
func (u UserDataPath) Location() (string, error) {
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
