package config

import (
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// Every field a preset or a login can touch survives a write and a read,
// including the optional credential page.
func TestFileRoundTripKeepsServiceIdentityAndSharedDefaults(t *testing.T) {
	dir := t.TempDir()
	want := File{
		CurrentContext: "team",
		Contexts: []NamedContext{
			setupFixture(),
			{Name: "personal", BaseURL: "https://caldav.wecom.work/", Auth: AuthConfig{Scheme: "basic", Username: "me@example.test"}},
		},
		Defaults: Defaults{Format: "table", PageSize: 50, Timeout: 45 * time.Second, MaxRetries: 5, ReadOnly: true},
	}
	if err := WriteFile(dir, want); err != nil {
		t.Fatal(err)
	}
	got, exists, err := ReadFile(dir)
	if err != nil || !exists {
		t.Fatal(exists, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip lost fields:\n got %+v\nwant %+v", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("write left temporary files behind: %v %v", entries, err)
	}
}

// A legacy flat file may carry the credential page too.
func TestLegacyFlatFileKeepsTheCredentialPage(t *testing.T) {
	dir := t.TempDir()
	flat := "server: https://service.example.test/deploy\nauth:\n  scheme: basic\n  username: member@example.test\n  credential_url: https://help.example.test/caldav\n"
	if err := os.WriteFile(ConfigFilePath(dir), []byte(flat), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := ReadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := setupFixture()
	want.Name = DefaultContextName
	if len(got.Contexts) != 1 || got.Contexts[0] != want || got.CurrentContext != DefaultContextName {
		t.Fatalf("legacy file parsed as %+v", got)
	}
}

// The file is replaced through a temporary file, so a write that cannot
// complete leaves the previous configuration readable and intact.
func TestFailedWriteLeavesThePreviousConfigIntact(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the current user cannot write to")
	}
	dir := t.TempDir()
	before := File{CurrentContext: "team", Contexts: []NamedContext{setupFixture()}}
	if err := WriteFile(dir, before); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	changed := before
	changed.Contexts = []NamedContext{{Name: "team", BaseURL: "https://changed.example.test"}}
	if err := WriteFile(dir, changed); err == nil {
		t.Fatal("write into a read-only directory succeeded")
	}
	after, _, err := ReadFile(dir)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("a failed write damaged the config: %+v %v", after, err)
	}
}
