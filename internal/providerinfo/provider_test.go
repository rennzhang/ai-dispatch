package providerinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executableFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveBinaryExplicitOverrideDoesNotFallback(t *testing.T) {
	configured := executableFixture(t, "omp")
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("AI_DISPATCH_OMP_BIN", missing)
	_, err := ResolveBinary("omp", configured)
	if err == nil {
		t.Fatal("invalid override fell back to configured binary")
	}
	if strings.Contains(err.Error(), missing) {
		t.Fatalf("error leaked private path: %v", err)
	}
	if !strings.Contains(err.Error(), "binary not found") {
		t.Fatalf("not classified as unavailable: %v", err)
	}
}

func TestResolveBinaryOverrideThenConfigThenPath(t *testing.T) {
	configured := executableFixture(t, "configured")
	override := executableFixture(t, "override")
	t.Setenv("AI_DISPATCH_OMP_BIN", override)
	got, err := ResolveBinary("omp", configured)
	if err != nil || got != override {
		t.Fatalf("override got=%q err=%v", got, err)
	}
	t.Setenv("AI_DISPATCH_OMP_BIN", "")
	got, err = ResolveBinary("omp", configured)
	if err != nil || got != configured {
		t.Fatalf("configured got=%q err=%v", got, err)
	}
	t.Setenv("PATH", filepath.Dir(override))
	if err := os.Rename(override, filepath.Join(filepath.Dir(override), "omp")); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveBinary("omp")
	if err != nil || filepath.Base(got) != "omp" || !filepath.IsAbs(got) {
		t.Fatalf("PATH got=%q err=%v", got, err)
	}
}

func TestResolveBinaryRejectsBrokenConfiguredPathAndUnknownProvider(t *testing.T) {
	t.Setenv("AI_DISPATCH_OMP_BIN", "")
	if _, err := ResolveBinary("omp", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("broken config fell back")
	}
	if _, err := ResolveBinary("unknown"); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestRegistryDoesNotExposeMutableSlices(t *testing.T) {
	spec, _ := Lookup("omp")
	spec.Surfaces[0] = "app"
	if Surfaces("omp")[0] != "cli" {
		t.Fatal("registry mutated")
	}
}
