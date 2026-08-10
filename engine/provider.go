package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Provider resolves a `copy` step's `from` name to a local path. It is how the
// engine stays ignorant of where a host keeps its content: PortForge registers a
// "rom" provider backed by its ROM library index, and another host might
// register a "depot" provider backed by a downloaded game depot.
//
// The returned path may be a file or a directory; the copy step handles both.
type Provider interface {
	Resolve(ctx context.Context, req ProviderRequest) (string, error)
}

// ProviderRequest describes what a copy step is asking for.
type ProviderRequest struct {
	// From is the provider name, i.e. the step's `from` value.
	From string
	// Src is the step's `src` after arg interpolation. Its meaning is the
	// provider's to define — PortForge treats it as a ROM title, and an empty
	// value as "any ROM this item depends on that is present locally".
	Src string
	// Args are the resolved install args for this run.
	Args map[string]string
	// Step is the full step, for providers that need more than Src.
	Step Step
}

// ProviderFunc adapts a plain function to the Provider interface.
type ProviderFunc func(ctx context.Context, req ProviderRequest) (string, error)

func (f ProviderFunc) Resolve(ctx context.Context, req ProviderRequest) (string, error) {
	return f(ctx, req)
}

// DirProvider returns a Provider that resolves Src as a path beneath root. It
// backs the CLI's --provider NAME=DIR flag and is useful for testing specs
// without a full host application. Paths escaping root are rejected.
func DirProvider(root string) Provider {
	return ProviderFunc(func(_ context.Context, req ProviderRequest) (string, error) {
		if req.Src == "" {
			return "", fmt.Errorf("provider %q requires a src", req.From)
		}
		clean := filepath.Clean(root)
		full := filepath.Join(clean, req.Src)
		rel, err := filepath.Rel(clean, full)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("provider %q: %q escapes %s", req.From, req.Src, clean)
		}
		if _, err := os.Stat(full); err != nil {
			return "", fmt.Errorf("provider %q: %w", req.From, err)
		}
		return full, nil
	})
}
