package chastisafe

import (
	"fmt"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCSCommandFitsDiscordLimits(t *testing.T) {
	command := csCommand()
	if command.Name != "cs" {
		t.Fatalf("command name = %q", command.Name)
	}
	if len(command.Options) > 25 {
		t.Fatalf("top-level options = %d", len(command.Options))
	}

	seen := make(map[string]bool)
	for _, option := range command.Options {
		if seen[option.Name] {
			t.Fatalf("duplicate top-level option %q", option.Name)
		}
		seen[option.Name] = true

		if option.Type != discordgo.ApplicationCommandOptionSubCommandGroup {
			continue
		}
		if len(option.Options) > 25 {
			t.Fatalf("group %q has %d subcommands", option.Name, len(option.Options))
		}
		subSeen := make(map[string]bool)
		for _, sub := range option.Options {
			if sub.Type != discordgo.ApplicationCommandOptionSubCommand {
				t.Fatalf("group %q contains a non-subcommand", option.Name)
			}
			if subSeen[sub.Name] {
				t.Fatalf("group %q has a duplicate subcommand %q", option.Name, sub.Name)
			}
			subSeen[sub.Name] = true
			if len(sub.Options) > 25 {
				t.Fatalf("%s %s has %d options", option.Name, sub.Name, len(sub.Options))
			}
			optionSeen := make(map[string]bool)
			for _, child := range sub.Options {
				if optionSeen[child.Name] {
					t.Fatalf("%s %s has a duplicate option %q", option.Name, sub.Name, child.Name)
				}
				optionSeen[child.Name] = true
			}
		}
	}
}

func TestSubcommandPathAndOptions(t *testing.T) {
	i := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type: discordgo.InteractionApplicationCommand,
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "cs",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{
					Name: "template",
					Type: discordgo.ApplicationCommandOptionSubCommandGroup,
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{
							Name: "view",
							Type: discordgo.ApplicationCommandOptionSubCommand,
							Options: []*discordgo.ApplicationCommandInteractionDataOption{
								{Name: "ref", Type: discordgo.ApplicationCommandOptionString, Value: "Steel"},
							},
						},
					},
				},
			},
		},
	}}

	if got := subcommandPath(i); got != "template view" {
		t.Fatalf("path = %q", got)
	}
	opts := subcommandOptions(i)
	if len(opts) != 1 || opts[0].Name != "ref" {
		t.Fatalf("options = %+v", opts)
	}
	if value, ok := stringOption(opts, "ref"); !ok || value != "Steel" {
		t.Fatalf("stringOption = %q, %t", value, ok)
	}
}

func TestFocusedOption(t *testing.T) {
	i := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type: discordgo.InteractionApplicationCommandAutocomplete,
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "cs",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{
					Name: "lock",
					Type: discordgo.ApplicationCommandOptionSubCommandGroup,
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{
							Name: "view",
							Type: discordgo.ApplicationCommandOptionSubCommand,
							Options: []*discordgo.ApplicationCommandInteractionDataOption{
								{Name: "ref", Type: discordgo.ApplicationCommandOptionString, Value: "Ste", Focused: true},
							},
						},
					},
				},
			},
		},
	}}
	name, value := focusedOption(i)
	if name != "ref" || value != "Ste" {
		t.Fatalf("focusedOption = %q, %q", name, value)
	}
	if !autocompleteMatches("Ste", "Steel (1)", "1") {
		t.Fatal("autocomplete did not match the label prefix")
	}
	if autocompleteMatches("zzz", "Steel (1)", "1") {
		t.Fatal("autocomplete matched an unrelated prefix")
	}
}

func TestCSCommandTextBudget(t *testing.T) {
	command := csCommand()
	total := len(command.Name) + len(command.Description)
	for _, option := range command.Options {
		total += optionTextSize(option)
	}
	if total > 8000 {
		t.Fatalf("command text size = %d, Discord limit is 8000", total)
	}
}

func optionTextSize(option *discordgo.ApplicationCommandOption) int {
	total := len(option.Name) + len(option.Description)
	for _, choice := range option.Choices {
		total += len(choice.Name) + len(fmt.Sprint(choice.Value))
	}
	for _, child := range option.Options {
		total += optionTextSize(child)
	}
	return total
}
