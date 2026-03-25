package telegram

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestDefaultCommands(t *testing.T) {
	commands := DefaultCommands()

	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if commands[0].Command != "start" || commands[0].Description != "查看使用说明" {
		t.Fatalf("unexpected start command: %#v", commands[0])
	}
	if commands[1].Command != "help" || commands[1].Description != "查看帮助" {
		t.Fatalf("unexpected help command: %#v", commands[1])
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
