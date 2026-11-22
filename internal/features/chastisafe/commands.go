package chastisafe

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	cs "grimoire/internal/chastisafe"
)

const modalNamespace = "chastisafe"

func loginCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "login",
		Description: "Link your ChastiSafe developer code to your Discord account",
	}
}

func logoutCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "logout",
		Description: "Remove your saved ChastiSafe developer code",
	}
}

func lockeesCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "lockees",
		Description: "List your ChastiSafe lockees and summarize their locks",
	}
}

func locksCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "locks",
		Description: "List your ChastiSafe lock templates sorted by popularity",
	}
}

func chastityCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "chastity",
		Description: "Show your own ChastiSafe locks",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Show your active locks",
			},
		},
	}
}

func (f *Feature) handleLoginCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if f.store == nil || f.box == nil {
		return bot.ReplyEphemeral(s, i, "ChastiSafe login is not configured on this bot.")
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			Title:    "ChastiSafe login",
			CustomID: modalNamespace + ":login",
			Components: []discordgo.MessageComponent{
				&discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						&discordgo.TextInput{
							CustomID:    "code",
							Label:       "Developer code",
							Style:       discordgo.TextInputShort,
							Placeholder: "Paste the code from your ChastiSafe profile",
							MinLength:   1,
							MaxLength:   512,
							Required:    true,
						},
					},
				},
			},
		},
	})
}

func (f *Feature) handleLoginModal(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}
	if !f.loginAttemptAllowed(userID) {
		return bot.ReplyEphemeral(s, i, "Too many login attempts. Wait a few minutes and try again.")
	}

	code := modalValue(i.ModalSubmitData().Components, "code")
	if strings.TrimSpace(code) == "" {
		return bot.ReplyEphemeral(s, i, "The developer code was empty. Run `/login` again.")
	}

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	identity, err := f.authenticate(ctx, userID, code)
	if err != nil {
		f.log.Info("chastisafe login failed", "user_id", userID, "err", err)
		switch {
		case errors.Is(err, errNotOwner):
			return bot.EditEphemeral(s, i,
				"That code belongs to a different ChastiSafe account.\n"+
					"Link your Discord account in ChastiSafe, then run `/login` again.")
		case errors.Is(err, cs.ErrUnauthorized):
			return bot.EditEphemeral(s, i, "ChastiSafe rejected that developer code. Check it and try again.")
		case errors.Is(err, errNotConfigured):
			return bot.EditEphemeral(s, i, "ChastiSafe login is not configured on this bot.")
		default:
			f.log.Warn("chastisafe login request failed", "user_id", userID, "err", err)
			return bot.EditEphemeral(s, i, "I could not reach ChastiSafe right now. Try again in a moment.")
		}
	}

	f.loginSucceeded(userID)
	f.log.Info("chastisafe login stored",
		"user_id", userID,
		"chastisafe_user", identity.User,
		"chastisafe_id", identity.ID.String(),
	)
	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"Logged in as **%s** (ID `%s`).\n"+
			"`/lockees`, `/locks` and `/chastity status` now use your code.",
		identity.User, identity.ID,
	))
}

func (f *Feature) handleLogoutCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	if err := f.forget(ctx, userID); err != nil {
		if errors.Is(err, errNotConfigured) {
			return bot.EditEphemeral(s, i, "ChastiSafe login is not configured on this bot.")
		}
		return err
	}
	f.log.Info("chastisafe logout", "user_id", userID)
	return bot.EditEphemeral(s, i, "Your saved ChastiSafe code has been removed.")
}

func (f *Feature) handleLockeesCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	client, _, err := f.clientFor(ctx, userID)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}
	locks, err := client.Lockees(ctx)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}

	return sendEphemeralChunks(s, i, lockeesText(locks))
}

func (f *Feature) handleLocksCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	client, _, err := f.clientFor(ctx, userID)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}
	templates, err := client.Templates(ctx)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}

	return sendEphemeralChunks(s, i, locksText(templates))
}

func (f *Feature) handleChastityCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
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

	client, cred, err := f.clientFor(ctx, userID)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}
	locks, err := client.Loaded(ctx)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}

	return sendEphemeralChunks(s, i, personalStatusText(locks, cred.ChastiSafeID))
}

// modalValue reads one text input from a submitted modal.
func modalValue(components []discordgo.MessageComponent, customID string) string {
	for _, component := range components {
		row, ok := component.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, child := range row.Components {
			input, ok := child.(*discordgo.TextInput)
			if ok && input.CustomID == customID {
				return input.Value
			}
		}
	}
	return ""
}
