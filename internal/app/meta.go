package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	cerrors "github.com/angelmsger/wecom-calendar-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func newMetaCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "meta",
		Short: "Read and write custom, agent-maintained event metadata",
		Long: "A free-form key-value layer attached to events by uid, organized under\n" +
			"namespaces. Use it for your own classification, notes, or links to tasks\n" +
			"in any project-management tool — the schema assumes none. Sync never\n" +
			"touches this layer, so re-syncing keeps your annotations.",
	}
	cmd.AddCommand(newMetaSetCmd(s), newMetaGetCmd(s), newMetaListCmd(s), newMetaDeleteCmd(s))
	return cmd
}

func newMetaSetCmd(s *appState) *cobra.Command {
	var source string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "set <uid> <namespace> <key> <value>",
		Short: "Set a metadata value on an event",
		Long: "Set metadata under (uid, namespace, key). The value is stored as JSON:\n" +
			"a value that already parses as JSON is kept verbatim, otherwise it is\n" +
			"stored as a JSON string.",
		Example: "  wecom-calendar-cli meta set <uid> classification category 评审\n" +
			"  wecom-calendar-cli meta set <uid> task feishu_project g-5980639611\n" +
			"  wecom-calendar-cli meta set <uid> note payload '{\"minutes\":30}'",
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			uid, ns, key, value := args[0], args[1], args[2], args[3]
			valueJSON := toJSONValue(value)
			// --dry-run previews the write and must work even under read-only mode
			// (it changes nothing), so it is checked before the write gate.
			if dryRun {
				return s.emit(map[string]any{"dry_run": true, "uid": uid, "namespace": ns,
					"key": key, "value": json.RawMessage(valueJSON), "source": source})
			}
			if err := s.guardWrite("meta set"); err != nil {
				return err
			}
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			exists, _ := st.EventExists(uid)
			if err := st.MetaSet(uid, ns, key, valueJSON, source, time.Now()); err != nil {
				return err
			}
			out := map[string]any{"uid": uid, "namespace": ns, "key": key,
				"value": json.RawMessage(valueJSON), "source": source, "status": "set"}
			if !exists {
				out["warning"] = "no live event with this uid in the store (metadata kept anyway)"
			}
			return s.emit(out)
		},
	}
	cmd.Flags().StringVar(&source, "source", "agent", "provenance tag: agent, user or auto")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without writing")
	return cmd
}

func newMetaGetCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:     "get <uid> [namespace] [key]",
		Short:   "Get metadata for one event",
		Example: "  wecom-calendar-cli meta get <uid>\n  wecom-calendar-cli meta get <uid> task",
		Args:    cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			uid := args[0]
			ns, key := "", ""
			if len(args) > 1 {
				ns = args[1]
			}
			if len(args) > 2 {
				key = args[2]
			}
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.MetaList(uid, ns, key)
			if err != nil {
				return err
			}
			return s.emitList(rows, pageInfo{})
		},
	}
}

func newMetaListCmd(s *appState) *cobra.Command {
	var uid, ns, key string
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List metadata across events, filtered by uid/namespace/key",
		Example: "  wecom-calendar-cli meta list --namespace task\n  wecom-calendar-cli meta list --key category",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			rows, err := st.MetaList(uid, ns, key)
			if err != nil {
				return err
			}
			return s.emitList(rows, pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringVar(&uid, "uid", "", "filter by event uid")
	f.StringVar(&ns, "namespace", "", "filter by namespace")
	f.StringVar(&key, "key", "", "filter by key")
	return cmd
}

func newMetaDeleteCmd(s *appState) *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "delete <uid> <namespace> <key>",
		Short: "Delete a metadata entry",
		Example: "  wecom-calendar-cli meta delete <uid> task feishu_project --dry-run\n" +
			"  wecom-calendar-cli meta delete <uid> task feishu_project --yes",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			uid, ns, key := args[0], args[1], args[2]
			// --dry-run resolves and previews the delete (including the current
			// value, if any) without removing anything, and works under read-only.
			if dryRun {
				st, err := s.openStore()
				if err != nil {
					return err
				}
				defer st.Close()
				rows, err := st.MetaList(uid, ns, key)
				if err != nil {
					return err
				}
				out := map[string]any{"dry_run": true, "uid": uid, "namespace": ns, "key": key}
				if len(rows) > 0 {
					out["status"] = "would_delete"
					out["current_value"] = rows[0].Value
				} else {
					out["status"] = "not_found"
				}
				return s.emit(out)
			}
			if err := s.guardWrite("meta delete"); err != nil {
				return err
			}
			// Destructive write: require --yes, or an interactive confirmation. A
			// non-interactive caller (agent/script) must pass --yes; we never block
			// on a prompt without a TTY. Mirrors the family's confirmation gate.
			if !yes {
				if !stdinIsTTY() {
					return cerrors.New(cerrors.CategoryUsage, "CONFIRM_REQUIRED",
						"meta delete removes data; pass --yes to confirm, or --dry-run to preview").
						WithHint("Agents and scripts should preview with --dry-run, then delete with --yes.")
				}
				fmt.Fprintf(os.Stderr, "Delete metadata %s / %s / %s? [y/N]: ", uid, ns, key)
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
					return s.emit(map[string]any{"uid": uid, "namespace": ns, "key": key, "status": "aborted"})
				}
			}
			st, err := s.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			n, err := st.MetaDelete(uid, ns, key)
			if err != nil {
				return err
			}
			status := "deleted"
			if n == 0 {
				status = "not_found"
			}
			return s.emit(map[string]any{"uid": uid, "namespace": ns,
				"key": key, "status": status})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "show what would be deleted without deleting")
	f.BoolVar(&yes, "yes", false, "confirm the deletion (required for non-interactive use)")
	return cmd
}

// guardWrite blocks a mutating command under the read-only posture.
func (s *appState) guardWrite(op string) error {
	if s.readOnly() {
		return cerrors.Newf(cerrors.CategoryPermission, "READONLY_BLOCKED",
			"%s is blocked by read-only mode", op).
			WithHint("Pass --allow-writes, or unset defaults.read_only / WECOM_CALENDAR_CLI_READ_ONLY.")
	}
	return nil
}

// toJSONValue returns value if it is already valid JSON, else a JSON string.
func toJSONValue(value string) string {
	if json.Valid([]byte(value)) {
		return value
	}
	b, _ := json.Marshal(value)
	return string(b)
}
