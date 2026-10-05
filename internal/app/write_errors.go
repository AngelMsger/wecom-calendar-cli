package app

import (
	"fmt"
	"strings"

	"github.com/angelmsger/wecom-calendar-cli/pkg/constants"
	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
)

// Write outcomes for the metadata layer.
//
// The sibling CLIs write to a remote service and read the resource back, so
// their rule covers a failed read after a successful write. This CLI's only
// writes are single SQLite statements against the local store: nothing is read
// back, the identity is the caller's own (uid, namespace, key), and a statement
// that returns an error did not apply. The one step that can still fail after
// the commit is printing the result, so the rule applies there.

// writeTarget names what a write addressed and the read that shows its state.
type writeTarget struct {
	description string
	readSteps   []string
}

func metaWriteTarget(uid, ns, key string) writeTarget {
	return writeTarget{
		description: fmt.Sprintf("metadata entry %s / %s / %s", uid, ns, key),
		readSteps: []string{constants.AppName + " meta get " +
			shellArgument(uid) + " " + shellArgument(ns) + " " + shellArgument(key)},
	}
}

func shellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func writeResultError(err error, code, message string, target writeTarget) *cerrors.CLIError {
	cause := cerrors.AsCLIError(err)
	out := cerrors.Wrap(err, cause.Category, code, message+": "+cause.Message).
		WithHint("Do not repeat the write; the local store already holds the change. Confirm it with the read command. After a delete, an empty list is the confirmation.").
		WithNextSteps(target.readSteps...)
	out.Retryable = false
	return out
}

// emitAfterWrite prints the result of a committed metadata write. A failure
// here must not read as a failed write: it keeps the entry's identity and the
// original cause, and points at a read instead of a replay.
func (s *appState) emitAfterWrite(v any, status string, target writeTarget) error {
	err := s.emit(v)
	if err == nil {
		return nil
	}
	return writeResultError(err, "WRITE_SUCCEEDED_OUTPUT_FAILED",
		"Write succeeded for "+target.description+" (status "+status+"), but printing its result failed", target)
}
