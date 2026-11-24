package chastisafe

import (
	"github.com/bwmarrin/discordgo"

	cs "github.com/zastraeaxyz/chastisafe-go"
)

const csCommandName = "cs"

func csCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        csCommandName,
		Description: "ChastiSafe account, templates, and running locks",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "login",
				Description: "Link your ChastiSafe developer code to your Discord account",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "logout",
				Description: "Remove your saved ChastiSafe developer code",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "account",
				Description: "Show the ChastiSafe account linked to your Discord user",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "lookup",
				Description: "Look up a ChastiSafe profile by user ID or username#number",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "user",
						Description: "ChastiSafe user ID or username#number",
						Required:    true,
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "status",
				Description: "Show your own active locks",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "lockees",
				Description: "List your lockees and summarize their locks",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "templates",
				Description: "List your lock templates sorted by popularity",
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
				Name:        "template",
				Description: "Create, inspect, update, or delete a lock template",
				Options:     templateSubcommands(),
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Name:        "locks",
				Description: "List running locks",
				Options: []*discordgo.ApplicationCommandOption{
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "scope",
						Description: "Which side of the lock to include",
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Mine as lockee", Value: string(cs.ScopeMine)},
							{Name: "Held as keyholder", Value: string(cs.ScopeLockees)},
							{Name: "All", Value: string(cs.ScopeAll)},
						},
					},
					{
						Type:        discordgo.ApplicationCommandOptionString,
						Name:        "state",
						Description: "Filter by state",
						Choices: []*discordgo.ApplicationCommandOptionChoice{
							{Name: "Active", Value: string(cs.StateActive)},
							{Name: "Frozen", Value: string(cs.StateFrozen)},
							{Name: "Ready", Value: string(cs.StateReady)},
						},
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionSubCommandGroup,
				Name:        "lock",
				Description: "Inspect, load, modify, or end a running lock",
				Options:     lockSubcommands(),
			},
		},
	}
}

func templateSubcommands() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "view",
			Description: "Show one template",
			Options: []*discordgo.ApplicationCommandOption{
				refOption("Template ID, exact name, or unique prefix"),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "create",
			Description: "Create a timer or card template",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Template name", Required: true},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "type",
					Description: "Template shape",
					Required:    true,
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "Timer", Value: "timer"},
						{Name: "Card", Value: "card"},
					},
				},
				{Type: discordgo.ApplicationCommandOptionString, Name: "description", Description: "Description"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "timer_min", Description: "Minimum timer, e.g. 1h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "timer_max", Description: "Maximum timer, e.g. 1d"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "cards", Description: "Card ranges, e.g. green:2,red:1"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "regularity", Description: "Card pick interval, e.g. 1h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "allowed_purposes", Description: "Example: chastity,task, or all"},
				boolOption("hidden", "Hide the template from public view"),
				boolOption("start_frozen", "Start loaded locks frozen"),
				boolOption("allow_updates", "Allow the lockee to update the lock"),
				boolOption("cumulative", "Make timer time cumulative"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "minimum_duration", Description: "Minimum lock duration, e.g. 30m"},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "clone",
			Description: "Copy one template into a new name",
			Options: []*discordgo.ApplicationCommandOption{
				refOption("Source template"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "New template name", Required: true},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "update",
			Description: "Change fields on one template",
			Options: []*discordgo.ApplicationCommandOption{
				refOption("Template to update"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "New name"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "description", Description: "New description"},
				boolOption("hidden", "Hide the template"),
				boolOption("start_frozen", "Start loaded locks frozen"),
				boolOption("allow_updates", "Allow lockee updates"),
				boolOption("cumulative", "Cumulative timer"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "timer_min", Description: "Minimum timer, e.g. 1h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "timer_max", Description: "Maximum timer, e.g. 1d"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "regularity", Description: "Card pick interval, e.g. 1h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "checkin_period", Description: "Check-in period, e.g. 6h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "checkin_tolerance", Description: "Check-in tolerance, e.g. 1h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "hygiene_period", Description: "Hygiene unlock period, e.g. 1d"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "hygiene_duration", Description: "Hygiene unlock duration, e.g. 15m"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "autoreset_period", Description: "Auto reset period, e.g. 1d"},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "autoreset_count", Description: "Auto reset count"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "minimum_duration", Description: "Minimum lock duration, e.g. 30m"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "allowed_purposes", Description: "Example: chastity,task, or all"},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "status",
					Description: "Template status",
					Choices: []*discordgo.ApplicationCommandOptionChoice{
						{Name: "Enabled", Value: "ENABLED"},
						{Name: "Disabled", Value: "DISABLED"},
					},
				},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "delete",
			Description: "Delete one template",
			Options: []*discordgo.ApplicationCommandOption{
				refOption("Template to delete"),
				confirmOption(),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "simulate",
			Description: "Preview a card template's duration and cards",
			Options: []*discordgo.ApplicationCommandOption{
				refOption("Template to simulate"),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "card",
			Description: "Change one card range on a template",
			Options: []*discordgo.ApplicationCommandOption{
				refOption("Template to edit"),
				cardOption(),
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "min", Description: "Minimum cards", Required: true},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "max", Description: "Maximum cards", Required: true},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "greens_required", Description: "Greens required for a green range"},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "dice_count", Description: "Dice count for a dice range"},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "dice_faces", Description: "Dice faces for a dice range"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "pink_script", Description: "Pink card script"},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "background",
			Description: "Change a template's background image name",
			Options: []*discordgo.ApplicationCommandOption{
				refOption("Template to edit"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "background", Description: "Background image name", Required: true},
			},
		},
	}
}

func lockSubcommands() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "view",
			Description: "Show one running lock",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Lock ID, name, lockee, or unique prefix"),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "load",
			Description: "Start a lock from one of your templates",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:         discordgo.ApplicationCommandOptionString,
					Name:         "template",
					Description:  "Template ID, name, or unique prefix",
					Required:     true,
					Autocomplete: true,
				},
				{Type: discordgo.ApplicationCommandOptionString, Name: "code", Description: "Lock code you want to set", Required: true},
				purposeOption(false),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "addtime",
			Description: "Add or remove time on a timer lock",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "amount", Description: "Example: 30m, 2h, 1d, or -15m", Required: true},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "settime",
			Description: "Replace remaining time on a timer lock",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "amount", Description: "Example: 30m, 2h, 1d", Required: true},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "freeze",
			Description: "Freeze or thaw a lock",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				{Type: discordgo.ApplicationCommandOptionBoolean, Name: "frozen", Description: "True to freeze, false to thaw", Required: true},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "hide",
			Description: "Hide or reveal a lock's timer or cards",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				{Type: discordgo.ApplicationCommandOptionBoolean, Name: "hidden", Description: "True to hide, false to reveal", Required: true},
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "purpose",
			Description: "Change a lock's purpose",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				purposeOption(true),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "cards",
			Description: "Add or remove cards",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				{Type: discordgo.ApplicationCommandOptionString, Name: "add", Description: "Cards to add, e.g. green:2,red:1"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "remove", Description: "Cards to remove, e.g. green:1"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "message", Description: "Optional update message"},
				boolOption("show", "Show the update to the lockee"),
				confirmOption(),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "settings",
			Description: "Change keyholder settings on a lock",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "max_picks", Description: "Maximum card picks"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "regularity", Description: "Card pick interval, e.g. 1h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "checkin_period", Description: "Check-in period, e.g. 6h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "checkin_tolerance", Description: "Check-in tolerance, e.g. 1h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "hygiene_period", Description: "Hygiene unlock period, e.g. 1d"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "hygiene_duration", Description: "Hygiene unlock duration, e.g. 15m"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "autoreset_period", Description: "Auto reset period, e.g. 1d"},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "autoreset_count", Description: "Auto reset count"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "remaining_time", Description: "Set remaining time, e.g. 2h"},
				{Type: discordgo.ApplicationCommandOptionString, Name: "add_time", Description: "Add time, e.g. 30m"},
				purposeOption(false),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "unlock",
			Description: "Unlock a ready lock as its keyholder",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				confirmOption(),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "reset",
			Description: "Restart a running lock",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				confirmOption(),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "abandon",
			Description: "End a running lock without revealing a code",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				confirmOption(),
			},
		},
		{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "split",
			Description: "Create fake locks from a card lock",
			Options: []*discordgo.ApplicationCommandOption{
				lockRefOption("Running lock"),
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "count", Description: "Number of fake locks", Required: true},
				confirmOption(),
			},
		},
	}
}

func refOption(description string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:         discordgo.ApplicationCommandOptionString,
		Name:         "ref",
		Description:  description,
		Required:     true,
		Autocomplete: true,
	}
}

func lockRefOption(description string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:         discordgo.ApplicationCommandOptionString,
		Name:         "ref",
		Description:  description,
		Required:     true,
		Autocomplete: true,
	}
}

func confirmOption() *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionBoolean,
		Name:        "confirm",
		Description: "Set to true to confirm this destructive action",
	}
}

func boolOption(name, description string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionBoolean,
		Name:        name,
		Description: description,
	}
}

func purposeOption(required bool) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionString,
		Name:        "purpose",
		Description: "Lock purpose",
		Required:    required,
		Choices: []*discordgo.ApplicationCommandOptionChoice{
			{Name: "Test", Value: string(cs.PurposeTest)},
			{Name: "Chastity", Value: string(cs.PurposeChastity)},
			{Name: "Bondage", Value: string(cs.PurposeBondage)},
			{Name: "Task", Value: string(cs.PurposeTask)},
			{Name: "Loyalty", Value: string(cs.PurposeLoyalty)},
			{Name: "Emergency", Value: string(cs.PurposeEmergency)},
		},
	}
}

func cardOption() *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type:        discordgo.ApplicationCommandOptionString,
		Name:        "card",
		Description: "Card to edit",
		Required:    true,
		Choices:     cardChoices(),
	}
}

func cardChoices() []*discordgo.ApplicationCommandOptionChoice {
	return []*discordgo.ApplicationCommandOptionChoice{
		{Name: "Green", Value: string(cs.CardGreen)},
		{Name: "Red", Value: string(cs.CardRed)},
		{Name: "Freeze", Value: string(cs.CardFreeze)},
		{Name: "Arctic", Value: string(cs.CardArctic)},
		{Name: "Reset", Value: string(cs.CardReset)},
		{Name: "Sticky", Value: string(cs.CardSticky)},
		{Name: "Stun", Value: string(cs.CardStun)},
		{Name: "Double up", Value: string(cs.CardDoubleUp)},
		{Name: "Dice", Value: string(cs.CardDice)},
		{Name: "Pink", Value: string(cs.CardPink)},
		{Name: "Go again", Value: string(cs.CardGoAgain)},
		{Name: "Yellow +1 red", Value: string(cs.CardYellowP1)},
		{Name: "Yellow +2 reds", Value: string(cs.CardYellowP2)},
		{Name: "Yellow +3 reds", Value: string(cs.CardYellowP3)},
		{Name: "Yellow -1 red", Value: string(cs.CardYellowM1)},
		{Name: "Yellow -2 reds", Value: string(cs.CardYellowM2)},
		{Name: "Yellow", Value: string(cs.CardYellow)},
		{Name: "Yellow UK", Value: string(cs.CardYellowUK)},
		{Name: "Yellow random", Value: string(cs.CardYellowRN)},
	}
}
