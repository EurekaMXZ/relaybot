package relay

import "testing"

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

	normalized, err := manager.Normalize(display)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got := manager.Hash(normalized); got != hash {
		t.Fatalf("Hash(Normalize(code)) = %q, want %q", got, hash)
	}
}

func TestNormalizeRejectsInvalidCode(t *testing.T) {
	manager := NewHMACCodeManager("secret")

	if _, err := manager.Normalize("relaybot_bad"); err == nil {
		t.Fatal("Normalize() expected error for short code")
	}
}
