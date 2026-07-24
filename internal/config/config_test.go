package config

import "testing"

// TestLayerFromVars covers the env/.env → layer mapping: known vars map to their
// field keys, empty values are skipped (so they do not shadow lower layers), and
// a supplied password implies the basic auth scheme.
func TestLayerFromVars(t *testing.T) {
	layer := layerFromVars(map[string]string{
		"WECOM_CALENDAR_SERVER":   "https://caldav.example.com/",
		"WECOM_CALENDAR_USERNAME": "a@b.com",
		"WECOM_CALENDAR_PASSWORD": "secret",
		"WECOM_CALENDAR_FORMAT":   "", // empty must not appear in the layer
	})
	if got := layer[fieldServer]; got != "https://caldav.example.com/" {
		t.Errorf("server = %q", got)
	}
	if _, ok := layer[fieldFormat]; ok {
		t.Error("empty FORMAT should be skipped, not stored")
	}
	if layer[fieldAuthScheme] != SchemeBasic {
		t.Errorf("a password should imply the basic scheme, got %q", layer[fieldAuthScheme])
	}
}

func TestNormalizeUsername(t *testing.T) {
	cases := map[string]string{
		"  Alice@Example.COM ": "alice@example.com", // email: trimmed + lowercased
		"NotAnEmail":           "NotAnEmail",        // non-email: casing preserved
		"  spaced  ":           "spaced",            // trimmed
	}
	for in, want := range cases {
		if got := NormalizeUsername(in); got != want {
			t.Errorf("NormalizeUsername(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeContextName(t *testing.T) {
	if got := NormalizeContextName("  Work "); got != "work" {
		t.Errorf("NormalizeContextName = %q, want %q", got, "work")
	}
}
