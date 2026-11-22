package stats

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
)

const (
	// CommandName is the slash command for analytics reports.
	CommandName = "stats"

	// interactionWork covers a slow Umami window without depending on the
	// bot's 30 second interaction context.
	interactionWork = 2 * time.Minute
)

func command() *discordgo.ApplicationCommand {
	manageGuild := int64(discordgo.PermissionManageGuild)

	return &discordgo.ApplicationCommand{
		Name:                     CommandName,
		Description:              "Post or preview bbdsmlr analytics",
		DefaultMemberPermissions: &manageGuild,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "post",
				Description: "Refresh the stats dashboard now",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "preview",
				Description: "Preview a report without posting it",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Show the stats schedule and last run",
			},
		},
	}
}

func (f *Feature) handleCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if i.Member == nil || i.Member.Permissions&discordgo.PermissionManageGuild == 0 {
		return bot.ReplyEphemeral(s, i, "You need the **Manage Server** permission to use stats reports.")
	}

	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return bot.ReplyEphemeral(s, i, "Use `/stats post`, `/stats preview`, or `/stats status`.")
	}

	sub := data.Options[0]
	switch sub.Name {
	case "post":
		return f.postNow(ctx, s, i)
	case "preview":
		return f.preview(ctx, s, i)
	case "status":
		return f.showStatus(ctx, s, i)
	default:
		return bot.ReplyEphemeral(s, i, "Unknown subcommand.")
	}
}

func (f *Feature) postNow(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if !f.enabled() {
		return bot.ReplyEphemeral(s, i, notConfiguredMessage)
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interactionWork)
	defer cancel()

	channelID, messageID, err := f.publish(workCtx, s)
	if err != nil {
		if errors.Is(err, errNoChannel) {
			return bot.EditEphemeral(s, i, "No stats channel yet. Run `/settings stats channel:#stats` first.")
		}
		return bot.EditEphemeral(s, i, "I could not refresh the stats dashboard: "+err.Error())
	}
	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"Stats dashboard refreshed in <#%s>: https://discord.com/channels/%s/%s/%s",
		channelID, f.guildID, channelID, messageID,
	))
}

func (f *Feature) preview(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if !f.enabled() {
		return bot.ReplyEphemeral(s, i, notConfiguredMessage)
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interactionWork)
	defer cancel()

	rep, err := f.collect(workCtx, time.Now())
	if err != nil {
		f.log.Warn("building a stats preview", "err", err)
		return bot.EditEphemeral(s, i, "I could not read Umami: "+err.Error())
	}
	return editEphemeralEmbed(s, i,
		"Preview only. Nothing was posted; `/stats post` refreshes the dashboard.",
		buildEmbeds(rep, f.siteURL),
	)
}

func (f *Feature) showStatus(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading stats settings: %w", err)
	}

	channel := "not set; run `/settings stats channel:#stats`"
	if settings != nil && settings.StatsChannelID != "" {
		channel = fmt.Sprintf("<#%s>", settings.StatsChannelID)
	}

	schedule := "off"
	if len(f.times) > 0 {
		parts := make([]string, 0, len(f.times))
		for _, at := range f.times {
			parts = append(parts, fmt.Sprintf("%02d:%02d", at.hour, at.minute))
		}
		schedule = strings.Join(parts, ", ") + " " + f.loc.String()
	}

	umami := "configured"
	if !f.enabled() {
		umami = "not configured (set `UMAMI_URL`, `UMAMI_WEBSITE_ID`, `UMAMI_API_KEY`)"
	}

	f.statusMu.Lock()
	next, last, lastErr := f.nextRunAt, f.lastRun, f.lastError
	f.statusMu.Unlock()

	nextText := "unknown"
	if !next.IsZero() {
		nextText = fmt.Sprintf("<t:%d:f> (<t:%d:R>)", next.Unix(), next.Unix())
	}
	lastText := "never"
	if !last.IsZero() {
		lastText = fmt.Sprintf("<t:%d:f> (<t:%d:R>)", last.Unix(), last.Unix())
	}
	if lastErr != "" {
		lastText += "\nLast error: " + truncate(lastErr, 400)
	}

	var out strings.Builder
	out.WriteString("**Stats reports**\n")
	fmt.Fprintf(&out, "Umami: %s\n", umami)
	fmt.Fprintf(&out, "Channel: %s\n", channel)
	fmt.Fprintf(&out, "Schedule: %s\n", schedule)
	fmt.Fprintf(&out, "Next run: %s\n", nextText)
	fmt.Fprintf(&out, "Last run: %s\n", lastText)
	if settings != nil && settings.StatsMessageID != "" && settings.StatsChannelID != "" {
		fmt.Fprintf(&out, "Dashboard: https://discord.com/channels/%s/%s/%s",
			f.guildID, settings.StatsChannelID, settings.StatsMessageID)
	}
	return bot.ReplyEphemeral(s, i, out.String())
}

// editEphemeralEmbed replaces a deferred reply with embeds, which the shared
// bot helpers cannot do because they only take text.
func editEphemeralEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, content string, embeds []*discordgo.MessageEmbed) error {
	edit := &discordgo.WebhookEdit{Content: &content}
	if embeds != nil {
		edit.Embeds = &embeds
	}
	_, err := s.InteractionResponseEdit(i.Interaction, edit)
	return err
}
