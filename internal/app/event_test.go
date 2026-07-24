package app

import (
	"testing"

	"github.com/angelmsger/wecom-calendar-cli/internal/store"
)

// TestMarkSelf: the configured account is flagged among attendees, matched
// case-insensitively for emails, so an agent can subtract "me".
func TestMarkSelf(t *testing.T) {
	att := []store.AttendeeOut{
		{Email: "Me@Example.COM"},
		{Email: "other@x.com"},
	}
	markSelf(att, "  me@example.com ")
	if !att[0].IsSelf {
		t.Error("the configured account should be flagged is_self")
	}
	if att[1].IsSelf {
		t.Error("a different attendee must not be flagged is_self")
	}
}

func TestMarkSelfNoIdentity(t *testing.T) {
	att := []store.AttendeeOut{{Email: "a@x.com"}}
	markSelf(att, "") // no configured username: flag nobody
	if att[0].IsSelf {
		t.Error("with no identity, no attendee should be flagged")
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" confirmed , ,tentative,")
	if len(got) != 2 || got[0] != "confirmed" || got[1] != "tentative" {
		t.Fatalf("splitCSV dropped/misparsed tokens: %v", got)
	}
	if splitCSV("   ") != nil {
		t.Error("blank input should yield nil, not an empty-token slice")
	}
}
