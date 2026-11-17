package welcome

import "github.com/bwmarrin/discordgo"

const (
	// CommandName is the slash command that manages the panel.
	CommandName = "welcome"
	// componentNamespace receives every custom ID starting with "welcome:".
	componentNamespace = "welcome"
	ackComponentID     = "welcome:ack"
)

func command() *discordgo.ApplicationCommand {
	manageGuild := int64(discordgo.PermissionManageGuild)

	return &discordgo.ApplicationCommand{
		Name:                     CommandName,
		Description:              "Manage the welcome panel and rules confirmation",
		DefaultMemberPermissions: &manageGuild,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "setup",
				Description: "Post or update the welcome panel",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionRole,
						Name:        "role",
						Description: "Role granted when a member confirms the rules",
						Required:    true,
					},
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "channel",
						Description: "Channel for the panel (defaults to the current channel)",
						ChannelTypes: []discordgo.ChannelType{
							discordgo.ChannelTypeGuildText,
							discordgo.ChannelTypeGuildNews,
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionChannel,
						Name:        "rules",
						Description: "Optional rules channel to link from the panel",
						ChannelTypes: []discordgo.ChannelType{
							discordgo.ChannelTypeGuildText,
							discordgo.ChannelTypeGuildNews,
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "body",
						Description: "Optional custom panel text",
						MaxLength:   3500,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "refresh",
				Description: "Repost or repair the welcome panel",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Show the welcome panel configuration",
			},
		},
	}
}
