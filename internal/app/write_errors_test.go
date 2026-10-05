package app

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// runMeta drives the real command tree against a throwaway config directory.
func runMeta(t *testing.T, cfgDir string, args ...string) error {
	t.Helper()
	t.Setenv(envNoSkillHint, "1")
	t.Setenv("WECOM_CALENDAR_CONTEXT", "") // a developer's context does not exist in cfgDir
	root, _ := newRootCmdWithState()
	root.SetArgs(append([]string{"--config", cfgDir, "--allow-writes", "meta"}, args...))
	return root.Execute()
}

func seedMeta(t *testing.T, cfgDir string) {
	t.Helper()
	st, err := store.Open(storePath(cfgDir))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.MetaSet("uid-1", "task", "link", `"T-1"`, "agent", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func metaRows(t *testing.T, cfgDir string) []store.MetaRow {
	t.Helper()
	st, err := store.Open(storePath(cfgDir))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.MetaList("uid-1", "task", "link", "")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// An unknown --format is only rejected while printing, after the statement has
// committed, so it is the reproducible way to fail the step that follows a write.
func TestMetaWriteSucceededOutputFailed(t *testing.T) {
	for _, operation := range []string{"set", "delete"} {
		t.Run(operation, func(t *testing.T) {
			cfgDir := t.TempDir()
			var err error
			switch operation {
			case "set":
				err = runMeta(t, cfgDir, "set", "uid-1", "task", "link", "T-1", "--format", "bogus")
			case "delete":
				seedMeta(t, cfgDir)
				err = runMeta(t, cfgDir, "delete", "uid-1", "task", "link", "--yes", "--format", "bogus")
			}
			ce := cerrors.AsCLIError(err)
			if ce == nil || ce.Code != "WRITE_SUCCEEDED_OUTPUT_FAILED" || ce.Retryable || ce.Category != cerrors.CategoryUsage {
				t.Fatalf("unexpected error: %+v", ce)
			}
			if !strings.Contains(ce.Message, "metadata entry uid-1 / task / link") || len(ce.NextSteps) != 1 ||
				ce.NextSteps[0] != "wecom-calendar-cli meta get 'uid-1' 'task' 'link'" {
				t.Fatalf("missing identity/read recovery: %+v", ce)
			}
			var cause *cerrors.CLIError
			if !errors.As(errors.Unwrap(ce), &cause) || cause.Code != "BAD_FORMAT" {
				t.Fatal("original output error was not preserved")
			}
			rows := metaRows(t, cfgDir)
			if operation == "set" && (len(rows) != 1 || string(rows[0].Value) != `"T-1"`) {
				t.Fatalf("set did not commit before the output failure: %+v", rows)
			}
			if operation == "delete" && len(rows) != 0 {
				t.Fatalf("delete did not commit before the output failure: %+v", rows)
			}
		})
	}
}

// Only a committed change is reported as a successful write.
func TestMetaOutputFailureWithoutWrite(t *testing.T) {
	t.Run("delete matched nothing", func(t *testing.T) {
		err := runMeta(t, t.TempDir(), "delete", "uid-1", "task", "link", "--yes", "--format", "bogus")
		if ce := cerrors.AsCLIError(err); ce == nil || ce.Code != "BAD_FORMAT" {
			t.Fatalf("unexpected error: %+v", ce)
		}
	})
	t.Run("blocked before the write", func(t *testing.T) {
		cfgDir := t.TempDir()
		t.Setenv(envNoSkillHint, "1")
		t.Setenv("WECOM_CALENDAR_CONTEXT", "")
		t.Setenv("WECOM_CALENDAR_CLI_READ_ONLY", "1")
		root, _ := newRootCmdWithState()
		root.SetArgs([]string{"--config", cfgDir, "meta", "set", "uid-1", "task", "link", "T-1", "--format", "bogus"})
		ce := cerrors.AsCLIError(root.Execute())
		if ce == nil || ce.Code != "READONLY_BLOCKED" || len(ce.NextSteps) == 0 ||
			ce.NextSteps[0] != "Add --allow-writes to the command line" {
			t.Fatalf("unexpected error: %+v", ce)
		}
		if _, err := os.Stat(storePath(cfgDir)); err == nil {
			t.Fatal("a blocked write created the store")
		}
	})
}

func TestWriteResultErrorPayload(t *testing.T) {
	ioErr := errors.New("write /dev/stdout: broken pipe")
	err := writeResultError(ioErr, "WRITE_SUCCEEDED_OUTPUT_FAILED",
		"Write succeeded for metadata entry u / n / k (status set), but printing its result failed",
		metaWriteTarget("it's", "n", "k"))
	if !errors.Is(err, ioErr) {
		t.Fatal("lost output failure cause")
	}
	data, marshalErr := json.Marshal(err.Payload())
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	var payload cerrors.Payload
	if unmarshalErr := json.Unmarshal(data, &payload); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}
	if payload.Error.Retryable || payload.Error.Category != cerrors.CategoryInternal ||
		!strings.Contains(payload.Error.Message, "broken pipe") ||
		payload.Error.NextSteps[0] != `wecom-calendar-cli meta get 'it'"'"'s' 'n' 'k'` {
		t.Fatalf("payload=%s", data)
	}
}
