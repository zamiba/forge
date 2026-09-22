package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

func resolveLocationType(name string) (string, error) {
	env, fallback := "", ""
	switch name {
	case "linuxConfig":
		env, fallback = "XDG_CONFIG_HOME", ".config"
	case "linuxData":
		env, fallback = "XDG_DATA_HOME", filepath.Join(".local", "share")
	default:
		return "", fmt.Errorf("userDataPaths: %q is not a location on this platform", name)
	}
	if dir := os.Getenv(env); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}
