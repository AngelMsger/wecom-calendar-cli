package store

import (
	"testing"
	"time"
)

// TestMetaValueLookupAndBatch covers the reverse-lookup (--value) and the
// batched MetaForUIDs used by --include-meta.
func TestMetaValueLookupAndBatch(t *testing.T) {
	st := openTemp(t)
	now := time.Unix(1_700_000_000, 0)
	for _, m := range []struct{ uid, ns, key, val string }{
		{"u1", "task", "feishu", `"g-123"`},
		{"u2", "task", "feishu", `"g-999"`},
		{"u3", "note", "ref", `"see g-123 for context"`},
	} {
		if err := st.MetaSet(m.uid, m.ns, m.key, m.val, "agent", now); err != nil {
			t.Fatal(err)
		}
	}

	// value substring matches the scalar (u1) and the mention inside text (u3).
	rows, err := st.MetaList("", "", "", "g-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("value filter want 2 (u1,u3), got %d", len(rows))
	}

	// value combines with key: only the task link, not the note.
	rows, err = st.MetaList("", "", "feishu", "g-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UID != "u1" {
		t.Fatalf("key+value want only u1, got %+v", rows)
	}

	byUID, err := st.MetaForUIDs([]string{"u1", "u2", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byUID["u1"]) != 1 || len(byUID["u2"]) != 1 || len(byUID["absent"]) != 0 {
		t.Fatalf("MetaForUIDs grouping wrong: %v", byUID)
	}
}
