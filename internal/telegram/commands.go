package telegram

import (
	"context"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func DefaultCommands() []models.BotCommand {
	return []models.BotCommand{
		{Command: "start", Description: "查看使用说明"},
		{Command: "help", Description: "查看帮助"},
	}
}

func SyncPrivateCommands(ctx context.Context, botClient *bot.Bot) error {
	_, err := botClient.SetMyCommands(ctx, privateCommandsParams())
	return err
}

func privateCommandsParams() *bot.SetMyCommandsParams {
	return &bot.SetMyCommandsParams{
		Commands: DefaultCommands(),
		Scope:    &models.BotCommandScopeAllPrivateChats{},
	}
}
