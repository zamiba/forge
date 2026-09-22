package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

func resolveLocationType(name string) (string, error) {
	switch name {
	case "macosApplicationSupport":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return "", fmt.Errorf("userDataPaths: %q is not a location on this platform", name)
}
