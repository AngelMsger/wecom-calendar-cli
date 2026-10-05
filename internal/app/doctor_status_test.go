package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompanionSkillDoctorCheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(envSkillLoaded, "")
	dir := filepath.Join(home, ".codex", "skills", "wecom-calendar")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	version := strings.TrimPrefix(embeddedSkillVersion(), "v")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nversion: "+version+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := companionSkillDoctorCheck(); got.Status != "installed_not_loaded" || !got.OK {
		t.Fatalf("installed check = %+v", got)
	}
	t.Setenv(envSkillLoaded, "0.0.0")
	if got := companionSkillDoctorCheck(); got.Status != "reload_required" {
		t.Fatalf("reload check = %+v", got)
	}
	t.Setenv(envSkillLoaded, version)
	if got := companionSkillDoctorCheck(); got.Status != "current" || !got.OK {
		t.Fatalf("loaded check = %+v", got)
	}
}
