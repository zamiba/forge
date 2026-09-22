package engine

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func resolveLocationType(name string) (string, error) {
	switch name {
	case "windowsRoaming":
		return fromEnv("APPDATA")
	case "windowsLocal":
		return fromEnv("LOCALAPPDATA")
	case "windowsDocuments":
		// The known folder, not %USERPROFILE%\Documents: Documents is often
		// redirected, to OneDrive above all, and the redirected place is
		// where the program writes.
		return windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	case "windowsSavedGames":
		return windows.KnownFolderPath(windows.FOLDERID_SavedGames, 0)
	}
	return "", fmt.Errorf("userDataPaths: %q is not a location on this platform", name)
}

func fromEnv(name string) (string, error) {
	if dir := os.Getenv(name); dir != "" {
		return dir, nil
	}
	return "", fmt.Errorf("userDataPaths: %%%s%% is not set", name)
}
