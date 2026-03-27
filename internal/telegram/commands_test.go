package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestDefaultCommands(t *testing.T) {
	commands := DefaultCommands()

	if len(commands) != 5 {
		t.Fatalf("expected 5 commands, got %d", len(commands))
	}
	if commands[0].Command != "start" || commands[0].Description != "查看使用说明" {
		t.Fatalf("unexpected start command: %#v", commands[0])
	}
	if commands[1].Command != "help" || commands[1].Description != "查看帮助" {
		t.Fatalf("unexpected help command: %#v", commands[1])
	}
	if commands[2].Command != "batch_start" {
		t.Fatalf("unexpected batch_start command: %#v", commands[2])
	}
	if commands[3].Command != "batch_done" {
		t.Fatalf("unexpected batch_done command: %#v", commands[3])
	}
	if commands[4].Command != "batch_cancel" {
		t.Fatalf("unexpected batch_cancel command: %#v", commands[4])
	}
}

func TestPrivateCommandsScope(t *testing.T) {
	params := privateCommandsParams()

	if params.LanguageCode != "" {
		t.Fatalf("expected empty language code, got %q", params.LanguageCode)
	}
	if _, ok := params.Scope.(*models.BotCommandScopeAllPrivateChats); !ok {
		t.Fatalf("expected all private chats scope, got %T", params.Scope)
	}
}
