package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestExtractClaimRelayInputsFromText(t *testing.T) {
	update := &models.Update{
		ID: 42,
		Message: &models.Message{
			Text: "帮我取回 relaybot_aZ09BcDeF123GhIjK456，再取 relaybot_Z9y8X7w6V5u4T3s2R1q0，谢谢",
			From: &models.User{ID: 1001},
			Chat: models.Chat{ID: 2002},
		},
	}

	claims := ExtractClaimRelayInputs(update)
	if len(claims) != 2 {
		t.Fatalf("expected 2 claims, got %d", len(claims))
	}
	if claims[0].RawCode != "relaybot_aZ09BcDeF123GhIjK456" {
		t.Fatalf("unexpected first code: %q", claims[0].RawCode)
	}
	if claims[1].RawCode != "relaybot_Z9y8X7w6V5u4T3s2R1q0" {
		t.Fatalf("unexpected second code: %q", claims[1].RawCode)
	}
	if claims[0].RequestUpdateID != claimRequestUpdateID(42, 0) {
		t.Fatalf("unexpected first request id: %d", claims[0].RequestUpdateID)
	}
	if claims[1].RequestUpdateID != claimRequestUpdateID(42, 1) {
		t.Fatalf("unexpected second request id: %d", claims[1].RequestUpdateID)
	}
}

func TestExtractClaimRelayInputsFromCaptionDedupesCodes(t *testing.T) {
	update := &models.Update{
		ID: 7,
		Message: &models.Message{
			Caption: "第一个 relaybot_aZ09BcDeF123GhIjK456，重复 relaybot_aZ09BcDeF123GhIjK456",
			From:    &models.User{ID: 1},
			Chat:    models.Chat{ID: 2},
		},
	}

	claims := ExtractClaimRelayInputs(update)
	if len(claims) != 1 {
		t.Fatalf("expected 1 deduplicated claim, got %d", len(claims))
	}
	if claims[0].RawCode != "relaybot_aZ09BcDeF123GhIjK456" {
		t.Fatalf("unexpected code: %q", claims[0].RawCode)
	}
}

func TestExtractClaimRelayInputReturnsFirstMatch(t *testing.T) {
	update := &models.Update{
		ID: 99,
		Message: &models.Message{
			Text: "relaybot_aZ09BcDeF123GhIjK456 和 relaybot_Z9y8X7w6V5u4T3s2R1q0",
			From: &models.User{ID: 1},
			Chat: models.Chat{ID: 2},
		},
	}

	claim, ok := ExtractClaimRelayInput(update)
	if !ok {
		t.Fatal("expected first claim to be extracted")
	}
	if claim.RawCode != "relaybot_aZ09BcDeF123GhIjK456" {
		t.Fatalf("unexpected first claim: %q", claim.RawCode)
	}
}

func TestExtractClaimRelayInputKeepsOriginalUpdateIDForSingleCode(t *testing.T) {
	update := &models.Update{
		ID: 123,
		Message: &models.Message{
			Text: "这里有一个 code: relaybot_aZ09BcDeF123GhIjK456",
			From: &models.User{ID: 1},
			Chat: models.Chat{ID: 2},
		},
	}

	claims := ExtractClaimRelayInputs(update)
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	if claims[0].RequestUpdateID != 123 {
		t.Fatalf("expected original update id 123, got %d", claims[0].RequestUpdateID)
	}
}
