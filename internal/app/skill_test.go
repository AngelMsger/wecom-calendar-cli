package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentIDs(t *testing.T) {
	t.Parallel()
	want := []string{
		"claude-code", "codex", "cursor", "agents", "gemini", "github-copilot",
		"opencode", "continue", "windsurf", "grok", "pi", "kilo", "roo",
	}
	ids := agentIDs()
	if len(ids) != len(want) {
		t.Fatalf("agentIDs() = %v (%d), want %d entries", ids, len(ids), len(want))
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("missing agent id %q", id)
		}
	}
}

func TestSkillVersionAlignment(t *testing.T) {
	dir := t.TempDir()
	write := func(version string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nversion: "+version+"\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	embedded := strings.TrimPrefix(embeddedSkillVersion(), "v")
	write(embedded)
	if got := inspectSkillInstall("test", dir); got.Alignment != "current" || got.Version == "" {
		t.Fatalf("current install = %+v", got)
	}
	write("0.0.0")
	if got := inspectSkillInstall("test", dir); got.Alignment != "outdated" {
		t.Fatalf("outdated install = %+v", got)
	}
	write("")
	if got := inspectSkillInstall("test", dir); got.Alignment != "unknown" {
		t.Fatalf("unversioned install = %+v", got)
	}
}

func TestCurrentSkillLoadState(t *testing.T) {
	t.Setenv(envSkillLoaded, strings.TrimPrefix(embeddedSkillVersion(), "v"))
	if got := currentSkillLoadState(); got.Status != "current" || !got.Loaded {
		t.Fatalf("current load = %+v", got)
	}
	t.Setenv(envSkillLoaded, "1")
	if got := currentSkillLoadState(); got.Status != "unknown" {
		t.Fatalf("legacy load = %+v", got)
	}
	t.Setenv(envSkillLoaded, "0.0.0")
	if got := currentSkillLoadState(); got.Status != "outdated" {
		t.Fatalf("outdated load = %+v", got)
	}
}

func TestAgentDests(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id              string
		wantHomeSuffix  string
		wantProjectPath string
	}{
		{"claude-code", filepath.Join(".claude", "skills", "wecom-calendar"), filepath.Join(".claude", "skills", "wecom-calendar")},
		{"codex", filepath.Join(".codex", "skills", "wecom-calendar"), filepath.Join(".agents", "skills", "wecom-calendar")},
		{"cursor", filepath.Join(".cursor", "skills", "wecom-calendar"), filepath.Join(".cursor", "skills", "wecom-calendar")},
		{"agents", filepath.Join(".agents", "skills", "wecom-calendar"), filepath.Join(".agents", "skills", "wecom-calendar")},
		{"gemini", filepath.Join(".gemini", "skills", "wecom-calendar"), filepath.Join(".gemini", "skills", "wecom-calendar")},
		{"github-copilot", filepath.Join(".copilot", "skills", "wecom-calendar"), filepath.Join(".agents", "skills", "wecom-calendar")},
		{"opencode", filepath.Join(".config", "opencode", "skills", "wecom-calendar"), filepath.Join(".opencode", "skills", "wecom-calendar")},
		{"continue", filepath.Join(".continue", "skills", "wecom-calendar"), filepath.Join(".continue", "skills", "wecom-calendar")},
		{"windsurf", filepath.Join(".codeium", "windsurf", "skills", "wecom-calendar"), filepath.Join(".windsurf", "skills", "wecom-calendar")},
		{"grok", filepath.Join(".grok", "skills", "wecom-calendar"), filepath.Join(".grok", "skills", "wecom-calendar")},
		{"pi", filepath.Join(".pi", "agent", "skills", "wecom-calendar"), filepath.Join(".pi", "skills", "wecom-calendar")},
		{"kilo", filepath.Join(".kilocode", "skills", "wecom-calendar"), filepath.Join(".kilocode", "skills", "wecom-calendar")},
		{"roo", filepath.Join(".roo", "skills", "wecom-calendar"), filepath.Join(".roo", "skills", "wecom-calendar")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			spec, ok := agentByID(tc.id)
			if !ok {
				t.Fatalf("agentSpec %q missing", tc.id)
			}
			projectPath, err := agentDest(spec, true)
			if err != nil {
				t.Fatal(err)
			}
			if projectPath != tc.wantProjectPath {
				t.Fatalf("project dest = %q, want %q", projectPath, tc.wantProjectPath)
			}
			homePath, err := agentDest(spec, false)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(homePath, tc.wantHomeSuffix) {
				t.Fatalf("home dest %q does not end with %q", homePath, tc.wantHomeSuffix)
			}
		})
	}
}
