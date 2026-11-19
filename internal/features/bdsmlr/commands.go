package bdsmlr

import "github.com/bwmarrin/discordgo"

const (
	// CommandName is the slash command for bdsmlr tools.
	CommandName = "bdsmlr"
)

func command() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        CommandName,
		Description: "bdsmlr account tools",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "invite",
				Description: "Send a BDSMLR invitation to an email address",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "email",
						Description: "Email address to invite",
						Required:    true,
					},
				},
			},
		},
	}
}
