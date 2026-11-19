package settings

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	"grimoire/internal/store"
)

// Feature implements the server settings command.
type Feature struct {
	guildID string
	store   *store.Store
	log     *slog.Logger
}

// New builds the settings feature for a single guild.
func New(guildID string, st *store.Store, log *slog.Logger) *Feature {
	if log == nil {
		log = slog.Default()
	}
	return &Feature{guildID: guildID, store: st, log: log}
}

// Register wires the feature into the bot.
func (f *Feature) Register(b *bot.Bot) {
	b.AddCommand(command(), f.handleCommand)
}

func (f *Feature) handleCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if i.Member == nil || i.Member.Permissions&discordgo.PermissionManageGuild == 0 {
		return bot.ReplyEphemeral(s, i, "You need the **Manage Server** permission to change settings.")
	}

	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return bot.ReplyEphemeral(s, i, "Use `/settings command-role` or `/settings status`.")
	}

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	sub := data.Options[0]
	switch sub.Name {
	case "command-role":
		return f.commandRole(ctx, s, i, sub.Options)
	case "status":
		return f.status(ctx, s, i)
	default:
		return bot.EditEphemeral(s, i, "Unknown subcommand.")
	}
}

func (f *Feature) commandRole(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	roleID := ""
	for _, opt := range opts {
		if opt.Name != "role" {
			continue
		}
		role := opt.RoleValue(s, f.guildID)
		if role == nil {
			return bot.EditEphemeral(s, i, "That role no longer exists. Pick another one.")
		}
		roleID = role.ID
	}

	if roleID == f.guildID {
		return bot.EditEphemeral(s, i, "Pick a role other than **@everyone**.")
	}
	if err := f.store.SetUserCommandRole(ctx, f.guildID, roleID); err != nil {
		return fmt.Errorf("saving the user command role: %w", err)
	}

	if roleID == "" {
		f.log.Info("member command role cleared")
		return bot.EditEphemeral(s, i, "Member commands are now limited to server admins.")
	}

	f.log.Info("member command role set", "role_id", roleID)
	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"Member commands are now available to <@&%s>. Server admins keep access.",
		roleID,
	))
}

func (f *Feature) status(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading settings: %w", err)
	}
	if settings == nil || settings.UserCommandRoleID == "" {
		return bot.EditEphemeral(s, i, "Member commands are limited to server admins. Set a role with `/settings command-role role:@Role`.")
	}

	roleID := settings.UserCommandRoleID
	if !f.roleExists(s, roleID) {
		return bot.EditEphemeral(s, i, fmt.Sprintf(
			"Member commands are set to a role that no longer exists (`%s`). Run `/settings command-role` to pick another.",
			roleID,
		))
	}

	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"Member commands are available to <@&%s>. Server admins keep access.",
		roleID,
	))
}

// roleExists prefers cached guild state and falls back to the REST API. If
// neither can answer, it reports true rather than claiming the role is gone.
func (f *Feature) roleExists(s *discordgo.Session, roleID string) bool {
	if _, err := s.State.Role(f.guildID, roleID); err == nil {
		return true
	}
	roles, err := s.GuildRoles(f.guildID)
	if err != nil {
		return true
	}
	for _, role := range roles {
		if role.ID == roleID {
			return true
		}
	}
	return false
}
