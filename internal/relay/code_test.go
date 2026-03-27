package relay

import (
	"strings"
	"testing"
)

func TestCodeManagerRoundTrip(t *testing.T) {
	manager := NewHMACCodeManager("secret")

	display, hash, hint, err := manager.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(hash) != 64 {
		t.Fatalf("unexpected hash length: %d", len(hash))
	}
	if len(hint) != 4 {
		t.Fatalf("unexpected code hint length: %d", len(hint))
	}
	if !strings.HasPrefix(display, "relaybot_") {
		t.Fatalf("unexpected code prefix: %q", display)
	}
	if strings.Contains(display, "-") {
		t.Fatalf("generated code should not contain hyphen groups: %q", display)
	}

	normalized, err := manager.Normalize(display)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if normalized != display {
		t.Fatalf("Normalize() = %q, want %q", normalized, display)
	}
	if got := manager.Hash(normalized); got != hash {
		t.Fatalf("Hash(Normalize(code)) = %q, want %q", got, hash)
	}
}

func TestNormalizeRejectsLegacyGroupedCode(t *testing.T) {
	manager := NewHMACCodeManager("secret")

	if _, err := manager.Normalize("relaybot_abcd-efgh-jkmn-pqrs"); err == nil {
		t.Fatal("Normalize() expected error for legacy grouped code")
	}
}

func TestNormalizeRejectsInvalidCode(t *testing.T) {
	manager := NewHMACCodeManager("secret")

	if _, err := manager.Normalize("relaybot_bad"); err == nil {
		t.Fatal("Normalize() expected error for short code")
	}
}
