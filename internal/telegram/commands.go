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
		{Command: "batch_start", Description: "开始批量上传"},
		{Command: "batch_done", Description: "完成批量上传并生成 code"},
		{Command: "batch_cancel", Description: "取消当前批量上传"},
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
