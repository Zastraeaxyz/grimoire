package rss

import "github.com/bwmarrin/discordgo"

const (
	// CommandName is the slash command that manages feed subscriptions.
	CommandName = "rss"
)

func command() *discordgo.ApplicationCommand {
	manageGuild := int64(discordgo.PermissionManageGuild)
	minBackfill := 0.0
	maxBackfill := 10.0

	return &discordgo.ApplicationCommand{
		Name:                     CommandName,
		Description:              "Follow RSS or Atom feeds and post new items",
		DefaultMemberPermissions: &manageGuild,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "add",
				Description: "Follow a feed and post new items to a channel",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "url",
						Description: "RSS or Atom feed URL",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "channel",
						Description: "Channel for new posts (defaults to the current channel)",
						ChannelTypes: []discordgo.ChannelType{
							discordgo.ChannelTypeGuildText,
							discordgo.ChannelTypeGuildNews,
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionInteger,
						Name:        "backfill",
						Description: "Recent items to post now (default 3)",
						MinValue:    &minBackfill,
						MaxValue:    maxBackfill,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "remove",
				Description: "Stop following a feed",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:         discordgo.ApplicationCommandOptionInteger,
						Name:         "feed",
						Description:  "Feed to remove",
						Required:     true,
						Autocomplete: true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "list",
				Description: "Show the feeds this server follows",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "test",
				Description: "Preview the newest item from a feed",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "url",
						Description: "RSS or Atom feed URL",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "check",
				Description: "Poll every feed now",
			},
		},
	}
}
