package settings

import "github.com/bwmarrin/discordgo"

const (
	// CommandName is the slash command that manages server settings.
	CommandName = "settings"
)

func command() *discordgo.ApplicationCommand {
	manageGuild := int64(discordgo.PermissionManageGuild)

	return &discordgo.ApplicationCommand{
		Name:                     CommandName,
		Description:              "Configure server settings",
		DefaultMemberPermissions: &manageGuild,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "command-role",
				Description: "Choose the role that may run member commands",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionRole,
						Name:        "role",
						Description: "Role allowed to run member commands (omit to clear)",
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Show the member command role",
			},
		},
	}
}
