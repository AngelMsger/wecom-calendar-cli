package app

import (
	"fmt"

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

// metaWriteTarget names a metadata entry and the read that shows its state.
func metaWriteTarget(uid, ns, key string) cerrors.WriteTarget {
	return cerrors.WriteTarget{
		Description: fmt.Sprintf("metadata entry %s / %s / %s", uid, ns, key),
		ReadSteps: []string{constants.AppName + " meta get " +
			cerrors.ShellArg(uid) + " " + cerrors.ShellArg(ns) + " " + cerrors.ShellArg(key)},
	}
}

// localWriteHint replaces the kit's wording, which is about remote state: here
// the change is already in the local store.
const localWriteHint = "Do not repeat the write; the local store already holds the change. " +
	"Confirm it with the read command. After a delete, an empty list is the confirmation."

// emitAfterWrite prints the result of a committed metadata write. A failure
// here must not read as a failed write: it is the kit's WriteSucceeded, which
// keeps the entry's identity, the cause's category and the cause itself, is
// not retryable and points at a read instead of a replay.
func (s *appState) emitAfterWrite(v any, status string, target cerrors.WriteTarget) error {
	if err := s.emit(v); err != nil {
		return writeOutputFailed(err, status, target)
	}
	return nil
}

func writeOutputFailed(err error, status string, target cerrors.WriteTarget) *cerrors.CLIError {
	target.Description += " (status " + status + ")"
	return cerrors.WriteSucceeded(err, "WRITE_SUCCEEDED_OUTPUT_FAILED", "printing its result failed", target).
		WithHint(localWriteHint)
}
