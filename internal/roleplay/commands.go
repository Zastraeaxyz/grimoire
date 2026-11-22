package roleplay

import (
	"context"
	"strings"

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

func promptCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "prompt",
		Description: "Show the persona system prompt.",
	}
}

// handlePromptCommand is admin-only. It shows the persona file plus the
// runtime tool notes, which is what an admin edits when tuning her.
func (f *Feature) handlePromptCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	content := strings.TrimSpace(f.basePrompt)
	if content == "" {
		content = "(empty)"
	}
	if len([]rune(content)) <= 1900 {
		return bot.EditEphemeral(s, i, content)
	}

	file := &discordgo.File{
		Name:        "system-prompt.md",
		ContentType: "text/markdown",
		Reader:      strings.NewReader(content),
	}
	note := "My system prompt is attached."
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &note,
		Files:   []*discordgo.File{file},
	})
	return err
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
