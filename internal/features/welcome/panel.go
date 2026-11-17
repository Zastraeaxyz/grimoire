package welcome

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/store"
)

// ensurePanel edits the stored panel message when it still exists and posts
// a fresh one when it does not. Safe to call on every reconnect.
func (f *Feature) ensurePanel(ctx context.Context, s *discordgo.Session, settings *store.GuildSettings) (*discordgo.Message, error) {
	if settings.WelcomeChannelID == "" {
		return nil, nil
	}

	embeds := f.buildEmbeds(s, settings)
	components := []discordgo.MessageComponent{ackRow()}
	content := ""

	if settings.PanelMessageID != "" {
		existing, err := s.ChannelMessage(settings.WelcomeChannelID, settings.PanelMessageID)
		if err == nil && existing.Author != nil && existing.Author.ID == s.State.User.ID {
			edited, editErr := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				Channel:    settings.WelcomeChannelID,
				ID:         settings.PanelMessageID,
				Content:    &content,
				Embeds:     &embeds,
				Components: &components,
			})
			if editErr != nil {
				return nil, fmt.Errorf("editing panel message: %w", editErr)
			}
			return edited, nil
		}
		if err != nil {
			f.log.Warn("stored panel message is unavailable; posting a new one",
				"channel_id", settings.WelcomeChannelID,
				"message_id", settings.PanelMessageID,
				"err", err,
			)
		}
	}

	posted, err := s.ChannelMessageSendComplex(settings.WelcomeChannelID, &discordgo.MessageSend{
		Embeds:     embeds,
		Components: components,
	})
	if err != nil {
		return nil, fmt.Errorf("posting panel message: %w", err)
	}

	if err := f.store.SetPanelMessage(ctx, settings.GuildID, posted.ID); err != nil {
		return nil, fmt.Errorf("saving panel message id: %w", err)
	}
	settings.PanelMessageID = posted.ID
	return posted, nil
}

// buildEmbeds returns an empty slice when no body was configured, in which
// case the panel is just the button.
func (f *Feature) buildEmbeds(s *discordgo.Session, settings *store.GuildSettings) []*discordgo.MessageEmbed {
	description := settings.PanelBody
	if description == "" {
		return []*discordgo.MessageEmbed{}
	}
	if settings.RulesChannelID != "" {
		description += "\n\nRules: <#" + settings.RulesChannelID + ">"
	}

	title := "Welcome"
	if guild, err := s.State.Guild(settings.GuildID); err == nil && guild.Name != "" {
		title = "Welcome to " + guild.Name
	}

	return []*discordgo.MessageEmbed{{
		Title:       title,
		Description: description,
		Color:       panelColor,
	}}
}

func ackRow() discordgo.ActionsRow {
	return discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "I have read the rules",
				Style:    discordgo.SuccessButton,
				CustomID: ackComponentID,
			},
		},
	}
}
