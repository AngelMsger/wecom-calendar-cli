package app

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain replaces the OS keychain with go-keyring's in-memory mock for the
// whole package, so no test, whatever its order, can read or write a real
// credential. Tests that store one start from a fresh mock of their own.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
