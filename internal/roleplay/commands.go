package roleplay

import (
	"context"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
)

func statusCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "chastity",
		Description: "Show your ChastiSafe lock status.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Show the locks where I am your keyholder.",
			},
		},
	}
}

func (f *Feature) handleStatusCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 || data.Options[0].Name != "status" {
		return bot.ReplyEphemeral(s, i, "Use `/chastity status`.")
	}

	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	return bot.EditEphemeral(s, i, f.statusFor(ctx, userID))
}

func (f *Feature) statusFor(ctx context.Context, userID string) string {
	if f == nil || f.locks == nil {
		return "ChastiSafe is not configured yet."
	}

	locks, err := f.locksFor(ctx, userID)
	if err != nil {
		f.log.Warn("loading chastisafe status", "err", err, "user_id", userID)
		return "I cannot reach ChastiSafe right now. Try again in a moment."
	}
	if len(locks) == 0 {
		return f.connectText(ctx)
	}
	return statusText(locks)
}
