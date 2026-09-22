//go:build !linux && !darwin && !windows

package engine

import "fmt"

func resolveLocationType(name string) (string, error) {
	return "", fmt.Errorf("userDataPaths: %q is not a location on this platform", name)
}
