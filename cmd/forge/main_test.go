package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zamiba/forge/engine"
)

// A --provider key is NAME or NAME.SRC, split at the first dot like the
// ${NAMEPath.SRC} reference it serves; the value is a path and nothing else,
// so a Windows path's colon needs no special handling.
func TestParseProviders(t *testing.T) {
	dir := t.TempDir()
	iso := filepath.Join(dir, "pikmin.iso")
	disc := filepath.Join(dir, "bd1.iso")
	for _, p := range []string{iso, disc} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	providers, err := parseProviders([]string{
		"rom=" + iso,
		"rom.Disc 1=" + disc,
		"rom.v1.0 disc=" + disc, // the src keeps its own dots
		"depot=" + dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(name, src string) string {
		t.Helper()
		got, err := providers[name].Resolve(context.Background(), engine.ProviderRequest{From: name, Src: src})
		if err != nil {
			t.Fatalf("%s %q: %v", name, src, err)
		}
		return got
	}
	if got := ask("rom", ""); got != iso {
		t.Errorf("rom unnamed = %q, want the file form", got)
	}
	if got := ask("rom", "Disc 1"); got != disc {
		t.Errorf("rom Disc 1 = %q, want the named form", got)
	}
	if got := ask("rom", "v1.0 disc"); got != disc {
		t.Errorf("rom v1.0 disc = %q, want the src split at the first dot only", got)
	}
	if got := ask("depot", "pikmin.iso"); got != iso {
		t.Errorf("depot pikmin.iso = %q, want the directory form", got)
	}

	for _, bad := range []string{"rom", "=x", "rom=", "rom.=" + iso, ".Disc 1=" + iso, "rom.Disc 1=" + filepath.Join(dir, "missing.iso"), "rom=no-such-file"} {
		if _, err := parseProviders([]string{bad}); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
