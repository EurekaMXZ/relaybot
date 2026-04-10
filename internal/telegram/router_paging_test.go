package telegram

import "testing"

func TestPageCallbackDataRoundtrip(t *testing.T) {
	raw := buildPageCallbackData(123, 2, 5, 456)
	parsed, ok := parsePageCallbackData(raw)
	if !ok {
		t.Fatalf("parsePageCallbackData(%q) returned not ok", raw)
	}

	if parsed.RelayID != 123 {
		t.Fatalf("RelayID = %d, want 123", parsed.RelayID)
	}
	if parsed.PageIndex != 2 {
		t.Fatalf("PageIndex = %d, want 2", parsed.PageIndex)
	}
	if parsed.PageTotal != 5 {
		t.Fatalf("PageTotal = %d, want 5", parsed.PageTotal)
	}
	if parsed.UserID != 456 {
		t.Fatalf("UserID = %d, want 456", parsed.UserID)
	}
}

func TestParsePageCallbackDataInvalid(t *testing.T) {
	cases := []string{
		"",
		"rp:1:2:3",
		"xx:1:2:3:4",
		"rp:0:1:1:1",
		"rp:1:0:1:1",
		"rp:1:1:0:1",
		"rp:1:1:1:0",
		"rp:a:b:c:d",
	}

	for _, raw := range cases {
		if _, ok := parsePageCallbackData(raw); ok {
			t.Fatalf("expected parsePageCallbackData(%q) to fail", raw)
		}
	}
}
