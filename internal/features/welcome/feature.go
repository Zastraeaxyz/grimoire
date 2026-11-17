package welcome

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	"grimoire/internal/store"
)

const (
	panelColor = 0x7C3AED
)

// Feature implements the welcome panel and rules confirmation.
type Feature struct {
	guildID string
	store   *store.Store
	log     *slog.Logger
}

// New builds the welcome feature for a single guild.
func New(guildID string, st *store.Store, log *slog.Logger) *Feature {
	if log == nil {
		log = slog.Default()
	}
	return &Feature{guildID: guildID, store: st, log: log}
}

// Register wires the feature into the bot.
func (f *Feature) Register(b *bot.Bot) {
	b.AddCommand(command(), f.handleCommand)
	b.AddComponent(componentNamespace, f.handleComponent)
	b.AddReadyHook(f.handleReady)
}

func (f *Feature) handleReady(ctx context.Context, s *discordgo.Session) error {
	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading welcome settings: %w", err)
	}
	if settings == nil || settings.WelcomeChannelID == "" || settings.BaseRoleID == "" {
		f.log.Info("welcome panel not configured yet; run /welcome setup")
		return nil
	}

	msg, err := f.ensurePanel(ctx, s, settings)
	if err != nil {
		return fmt.Errorf("ensuring welcome panel: %w", err)
	}
	if msg != nil {
		f.log.Info("welcome panel ready",
			"channel_id", settings.WelcomeChannelID,
			"message_id", msg.ID,
		)
	}
	return nil
}

func (f *Feature) handleCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if i.Member == nil || i.Member.Permissions&discordgo.PermissionManageGuild == 0 {
		return bot.ReplyEphemeral(s, i, "You need the **Manage Server** permission to configure the welcome panel.")
	}

	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return bot.ReplyEphemeral(s, i, "Use `/welcome setup` to configure the panel.")
	}

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	sub := data.Options[0]
	switch sub.Name {
	case "setup":
		return f.setup(ctx, s, i, sub.Options)
	case "refresh":
		return f.refresh(ctx, s, i)
	case "status":
		return f.status(ctx, s, i)
	default:
		return bot.EditEphemeral(s, i, "Unknown subcommand.")
	}
}

func (f *Feature) setup(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	channelID := i.ChannelID
	roleID := ""
	rulesID := ""
	body := ""

	for _, opt := range opts {
		switch opt.Name {
		case "role":
			if role := opt.RoleValue(s, f.guildID); role != nil {
				roleID = role.ID
			}
		case "channel":
			if ch := opt.ChannelValue(s); ch != nil {
				channelID = ch.ID
			}
		case "rules":
			if ch := opt.ChannelValue(s); ch != nil {
				rulesID = ch.ID
			}
		case "body":
			body = strings.TrimSpace(opt.StringValue())
		}
	}

	if roleID == "" {
		return bot.EditEphemeral(s, i, "Pick a role to grant with the `role` option.")
	}
	if roleID == f.guildID {
		return bot.EditEphemeral(s, i, "Pick a role other than **@everyone**.")
	}
	if err := f.validateChannel(s, channelID); err != nil {
		return bot.EditEphemeral(s, i, err.Error())
	}
	if err := f.validateRole(s, roleID); err != nil {
		return bot.EditEphemeral(s, i, err.Error())
	}

	old, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading welcome settings: %w", err)
	}

	settings := &store.GuildSettings{
		GuildID:          f.guildID,
		WelcomeChannelID: channelID,
		BaseRoleID:       roleID,
	}
	if old != nil {
		settings.RulesChannelID = old.RulesChannelID
		settings.PanelBody = old.PanelBody
		settings.PanelMessageID = old.PanelMessageID
	}
	if rulesID != "" {
		settings.RulesChannelID = rulesID
	}
	if body != "" {
		settings.PanelBody = body
	}

	// Moving the panel to another channel: retire the old message so the
	// server does not end up with two live panels.
	if old != nil && old.WelcomeChannelID != "" && old.WelcomeChannelID != channelID {
		if old.PanelMessageID != "" {
			f.deletePanel(s, old.WelcomeChannelID, old.PanelMessageID)
		}
		settings.PanelMessageID = ""
	}

	if err := f.store.SaveGuildSettings(ctx, settings); err != nil {
		return fmt.Errorf("saving welcome settings: %w", err)
	}

	msg, err := f.ensurePanel(ctx, s, settings)
	if err != nil {
		return fmt.Errorf("posting welcome panel: %w", err)
	}

	reply := fmt.Sprintf("Welcome panel is live in <#%s>.", channelID)
	if link := messageLink(f.guildID, channelID, msg); link != "" {
		reply += "\n" + link
	}
	reply += fmt.Sprintf("\nBase role: <@&%s>\nPress the button once yourself to confirm it grants the role.", roleID)
	return bot.EditEphemeral(s, i, reply)
}

func (f *Feature) refresh(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading welcome settings: %w", err)
	}
	if settings == nil || settings.WelcomeChannelID == "" || settings.BaseRoleID == "" {
		return bot.EditEphemeral(s, i, "The panel is not configured yet. Run `/welcome setup role:@Member channel:#welcome` first.")
	}

	msg, err := f.ensurePanel(ctx, s, settings)
	if err != nil {
		return fmt.Errorf("refreshing welcome panel: %w", err)
	}

	reply := "Welcome panel refreshed."
	if link := messageLink(f.guildID, settings.WelcomeChannelID, msg); link != "" {
		reply = "Welcome panel refreshed: " + link
	}
	return bot.EditEphemeral(s, i, reply)
}

func (f *Feature) status(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading welcome settings: %w", err)
	}
	if settings == nil {
		return bot.EditEphemeral(s, i, "Not configured yet. Run `/welcome setup role:@Member channel:#welcome`.")
	}

	panel := "not posted"
	if settings.PanelMessageID != "" {
		panel = fmt.Sprintf("<#%s> ([jump](%s))",
			settings.WelcomeChannelID,
			messageURL(f.guildID, settings.WelcomeChannelID, settings.PanelMessageID),
		)
	}

	rules := "not linked"
	if settings.RulesChannelID != "" {
		rules = "<#" + settings.RulesChannelID + ">"
	}

	count, err := f.store.CountAcknowledgements(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("counting acknowledgements: %w", err)
	}

	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"**Welcome configuration**\n"+
			"Panel: %s\n"+
			"Channel: <#%s>\n"+
			"Base role: <@&%s>\n"+
			"Rules channel: %s\n"+
			"Confirmations recorded: %d",
		panel,
		settings.WelcomeChannelID,
		settings.BaseRoleID,
		rules,
		count,
	))
}

func (f *Feature) handleComponent(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if i.MessageComponentData().CustomID != ackComponentID {
		return nil
	}
	if i.Member == nil || i.Member.User == nil {
		return nil
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	user := i.Member.User
	if user.Bot {
		return bot.EditEphemeral(s, i, "Bots do not need the member role.")
	}

	settings, err := f.store.GetGuildSettings(ctx, i.GuildID)
	if err != nil {
		return fmt.Errorf("loading welcome settings: %w", err)
	}
	if settings == nil || settings.BaseRoleID == "" {
		return bot.EditEphemeral(s, i, "The welcome panel is not configured yet. An admin needs to run `/welcome setup`.")
	}

	if hasRole(i.Member.Roles, settings.BaseRoleID) {
		return bot.EditEphemeral(s, i, "You already have access. Nothing else to do.")
	}

	// Always grant from Discord's live state. Acknowledgements are an audit
	// trail only, so members who left and rejoined can confirm again.
	if err := s.GuildMemberRoleAdd(i.GuildID, user.ID, settings.BaseRoleID); err != nil {
		f.log.Error("granting member role",
			"user_id", user.ID,
			"role_id", settings.BaseRoleID,
			"err", err,
		)
		return bot.EditEphemeral(s, i, roleErrorMessage(err))
	}

	if err := f.store.RecordAcknowledgement(ctx, i.GuildID, user.ID); err != nil {
		// The role is already granted; an audit write failure must not fail
		// the interaction.
		f.log.Warn("recording acknowledgement", "user_id", user.ID, "err", err)
	}

	f.log.Info("member confirmed rules", "user_id", user.ID, "role_id", settings.BaseRoleID)
	return bot.EditEphemeral(s, i, "You now have access. Welcome!")
}

func (f *Feature) validateChannel(s *discordgo.Session, channelID string) error {
	ch, err := s.Channel(channelID)
	if err != nil {
		return fmt.Errorf("I cannot see <#%s>. Check my channel permissions.", channelID)
	}
	if ch.Type != discordgo.ChannelTypeGuildText && ch.Type != discordgo.ChannelTypeGuildNews {
		return errors.New("Pick a text channel for the panel.")
	}

	perms, err := s.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return fmt.Errorf("checking my permissions in <#%s>: %w", channelID, err)
	}
	required := int64(discordgo.PermissionViewChannel |
		discordgo.PermissionSendMessages |
		discordgo.PermissionEmbedLinks |
		discordgo.PermissionManageMessages |
		discordgo.PermissionManageRoles)
	if perms&required != required {
		return fmt.Errorf("I need View Channel, Send Messages, Embed Links, Manage Messages and Manage Roles in <#%s>.", channelID)
	}
	return nil
}

func (f *Feature) validateRole(s *discordgo.Session, roleID string) error {
	guild, err := s.State.Guild(f.guildID)
	if err != nil {
		return fmt.Errorf("reading guild roles: %w", err)
	}

	var target *discordgo.Role
	for _, role := range guild.Roles {
		if role.ID == roleID {
			target = role
			break
		}
	}
	if target == nil {
		return errors.New("That role no longer exists. Pick another one.")
	}

	me, err := s.GuildMember(f.guildID, s.State.User.ID)
	if err != nil {
		return fmt.Errorf("reading my membership: %w", err)
	}

	highest := 0
	for _, roleID := range me.Roles {
		for _, role := range guild.Roles {
			if role.ID == roleID && role.Position > highest {
				highest = role.Position
			}
		}
	}
	if target.Position >= highest {
		return fmt.Errorf("My role sits at or below <@&%s>, so I cannot assign it. Move my role above it in Server Settings > Roles.", roleID)
	}
	return nil
}

func (f *Feature) deletePanel(s *discordgo.Session, channelID, messageID string) {
	if err := s.ChannelMessageDelete(channelID, messageID); err != nil {
		f.log.Warn("deleting old panel", "message_id", messageID, "err", err)
	}
}

func hasRole(roles []string, roleID string) bool {
	for _, id := range roles {
		if id == roleID {
			return true
		}
	}
	return false
}

func roleErrorMessage(err error) string {
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Message != nil {
		switch restErr.Message.Code {
		case discordgo.ErrCodeMissingPermissions, discordgo.ErrCodeMissingAccess:
			return "I could not grant the role. An admin needs to check that I have **Manage Roles** and that my role sits above the base role."
		case discordgo.ErrCodeUnknownRole:
			return "That role no longer exists. An admin needs to run `/welcome setup` again."
		case discordgo.ErrCodeUnknownMember:
			return "I could not find you in the server. Try again in a moment."
		}
	}
	return "I could not grant the role. An admin needs to check my role permissions."
}

func messageURL(guildID, channelID, messageID string) string {
	if guildID == "" || channelID == "" || messageID == "" {
		return ""
	}
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}

func messageLink(guildID, channelID string, msg *discordgo.Message) string {
	if msg == nil {
		return ""
	}
	return messageURL(guildID, channelID, msg.ID)
}
