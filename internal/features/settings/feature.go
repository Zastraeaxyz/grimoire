package settings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	"grimoire/internal/buildinfo"
	"grimoire/internal/store"
)

// metadataColor matches the welcome panel so server output stays consistent.
const metadataColor = 0x7C3AED

// Feature implements the server settings command.
type Feature struct {
	guildID   string
	store     *store.Store
	log       *slog.Logger
	startedAt time.Time

	metaMu     sync.Mutex
	metaPosted bool
}

// New builds the settings feature for a single guild.
func New(guildID string, st *store.Store, log *slog.Logger) *Feature {
	if log == nil {
		log = slog.Default()
	}
	return &Feature{
		guildID:   guildID,
		store:     st,
		log:       log,
		startedAt: time.Now().UTC(),
	}
}

// Register wires the feature into the bot.
func (f *Feature) Register(b *bot.Bot) {
	b.AddCommand(command(), f.handleCommand)
	b.AddReadyHook(f.handleReady)
}

// handleReady posts the build metadata once per process start. A failed post
// leaves the flag unset so the next READY retries; successful posts are not
// repeated on reconnects.
func (f *Feature) handleReady(ctx context.Context, s *discordgo.Session) error {
	f.metaMu.Lock()
	posted := f.metaPosted
	f.metaMu.Unlock()
	if posted {
		return nil
	}

	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading bot metadata settings: %w", err)
	}
	if settings == nil || settings.BotMetaChannelID == "" {
		f.log.Debug("bot metadata channel not configured; skipping startup post")
		return nil
	}

	if _, err := s.ChannelMessageSendEmbed(settings.BotMetaChannelID, f.metadataEmbed()); err != nil {
		return fmt.Errorf("posting build metadata: %w", err)
	}

	f.metaMu.Lock()
	f.metaPosted = true
	f.metaMu.Unlock()
	f.log.Info("posted build metadata",
		"channel_id", settings.BotMetaChannelID,
		"version", buildinfo.Version(),
	)
	return nil
}

func (f *Feature) handleCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if i.Member == nil || i.Member.Permissions&discordgo.PermissionManageGuild == 0 {
		return bot.ReplyEphemeral(s, i, "You need the **Manage Server** permission to change settings.")
	}

	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return bot.ReplyEphemeral(s, i, "Use `/settings command-role`, `/settings bot-meta`, `/settings stats`, or `/settings status`.")
	}

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	sub := data.Options[0]
	switch sub.Name {
	case "command-role":
		return f.commandRole(ctx, s, i, sub.Options)
	case "bot-meta":
		return f.botMeta(ctx, s, i, sub.Options)
	case "stats":
		return f.statsChannel(ctx, s, i, sub.Options)
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

func (f *Feature) botMeta(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	channelID := ""
	for _, opt := range opts {
		if opt.Name != "channel" {
			continue
		}
		ch := opt.ChannelValue(s)
		if ch == nil {
			return bot.EditEphemeral(s, i, "That channel no longer exists. Pick another one.")
		}
		channelID = ch.ID
	}

	if channelID != "" {
		if err := f.validateChannel(s, channelID, "build metadata"); err != nil {
			return bot.EditEphemeral(s, i, err.Error())
		}
	}
	if err := f.store.SetBotMetaChannel(ctx, f.guildID, channelID); err != nil {
		return fmt.Errorf("saving the bot metadata channel: %w", err)
	}

	if channelID == "" {
		f.log.Info("bot metadata channel cleared")
		return bot.EditEphemeral(s, i, "Build metadata posts are now off.")
	}

	msg, err := s.ChannelMessageSendEmbed(channelID, f.metadataEmbed())
	if err != nil {
		return fmt.Errorf("posting build metadata: %w", err)
	}

	// The test post covers this process, so a reconnect does not repeat it.
	f.metaMu.Lock()
	f.metaPosted = true
	f.metaMu.Unlock()

	f.log.Info("bot metadata channel set", "channel_id", channelID)
	reply := fmt.Sprintf(
		"Build metadata will be posted in <#%s> whenever I start.\nPosted now: https://discord.com/channels/%s/%s/%s",
		channelID, f.guildID, channelID, msg.ID,
	)
	return bot.EditEphemeral(s, i, reply)
}

// statsChannel points the stats dashboard at a channel. Reports are posted and
// edited there on the configured schedule.
func (f *Feature) statsChannel(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	channelID := ""
	for _, opt := range opts {
		if opt.Name != "channel" {
			continue
		}
		ch := opt.ChannelValue(s)
		if ch == nil {
			return bot.EditEphemeral(s, i, "That channel no longer exists. Pick another one.")
		}
		channelID = ch.ID
	}

	if channelID != "" {
		if err := f.validateChannel(s, channelID, "stats reports"); err != nil {
			return bot.EditEphemeral(s, i, err.Error())
		}
	}
	if err := f.store.SetStatsChannel(ctx, f.guildID, channelID); err != nil {
		return fmt.Errorf("saving the stats channel: %w", err)
	}

	if channelID == "" {
		f.log.Info("stats channel cleared")
		return bot.EditEphemeral(s, i, "Stats reports are now off. `/stats post` still works on demand.")
	}

	f.log.Info("stats channel set", "channel_id", channelID)
	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"Stats reports will be posted in <#%s>.\nUse `/stats post` to fill the dashboard now, or `/stats preview` to check the data first.",
		channelID,
	))
}

func (f *Feature) metadataEmbed() *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title: "Grimoire online",
		Color: metadataColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Version",
				Value:  buildinfo.Version(),
				Inline: true,
			},
			{
				Name:   "Started",
				Value:  fmt.Sprintf("<t:%d:f>", f.startedAt.Unix()),
				Inline: true,
			},
		},
	}
}

// validateChannel checks that the bot can actually post in a channel before
// saving it.
func (f *Feature) validateChannel(s *discordgo.Session, channelID, purpose string) error {
	ch, err := s.Channel(channelID)
	if err != nil {
		return fmt.Errorf("I cannot see <#%s>. Check my channel permissions.", channelID)
	}
	if ch.Type != discordgo.ChannelTypeGuildText && ch.Type != discordgo.ChannelTypeGuildNews {
		return fmt.Errorf("Pick a text channel for %s.", purpose)
	}

	perms, err := s.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return fmt.Errorf("checking my permissions in <#%s>: %w", channelID, err)
	}
	required := int64(discordgo.PermissionViewChannel |
		discordgo.PermissionSendMessages |
		discordgo.PermissionEmbedLinks)
	if perms&required != required {
		return fmt.Errorf("I need View Channel, Send Messages and Embed Links in <#%s>.", channelID)
	}
	return nil
}

func (f *Feature) status(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading settings: %w", err)
	}

	role := "server admins only"
	if settings != nil && settings.UserCommandRoleID != "" {
		if f.roleExists(s, settings.UserCommandRoleID) {
			role = fmt.Sprintf("<@&%s>", settings.UserCommandRoleID)
		} else {
			role = fmt.Sprintf("missing role `%s`; run `/settings command-role` again", settings.UserCommandRoleID)
		}
	}

	meta := "off"
	if settings != nil && settings.BotMetaChannelID != "" {
		meta = fmt.Sprintf("<#%s>", settings.BotMetaChannelID)
	}

	stats := "off"
	if settings != nil && settings.StatsChannelID != "" {
		stats = fmt.Sprintf("<#%s>", settings.StatsChannelID)
	}

	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"**Server settings**\n"+
			"Member commands: %s\n"+
			"Build metadata channel: %s\n"+
			"Stats channel: %s",
		role, meta, stats,
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
