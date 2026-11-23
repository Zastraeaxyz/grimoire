package main

import (
	"os"
	"path/filepath"
	"testing"

	"grimoire/internal/config"
)

func TestLoadRoleplayAbout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "about.md")
	if err := os.WriteFile(path, []byte("  I am {{bot}}.  \n"), 0o600); err != nil {
		t.Fatalf("writing about file: %v", err)
	}

	about, err := loadRoleplayAbout(config.RoleplayConfig{
		AboutFile: path,
		BotName:   "Raven",
	})
	if err != nil {
		t.Fatalf("loadRoleplayAbout: %v", err)
	}
	if about != "I am Raven." {
		t.Fatalf("about = %q", about)
	}
}

func TestLoadRoleplayAboutRejectsMissingAndEmpty(t *testing.T) {
	if _, err := loadRoleplayAbout(config.RoleplayConfig{
		AboutFile: filepath.Join(t.TempDir(), "missing.md"),
	}); err == nil {
		t.Fatal("expected an error for a missing file")
	}

	empty := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(empty, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("writing about file: %v", err)
	}
	if _, err := loadRoleplayAbout(config.RoleplayConfig{AboutFile: empty}); err == nil {
		t.Fatal("expected an error for an empty file")
	}
}
