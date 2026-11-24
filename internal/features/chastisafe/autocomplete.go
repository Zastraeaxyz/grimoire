package chastisafe

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	cs "github.com/zastraeaxyz/chastisafe-go"
)

// handleCSAutocomplete answers template and lock reference suggestions.
func (f *Feature) handleCSAutocomplete(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	path := subcommandPath(i)
	focused, current := focusedOption(i)
	if focused == "" {
		return bot.RespondAutocomplete(s, i, nil)
	}

	var wantTemplates bool
	switch {
	case focused == "template":
		wantTemplates = true
	case focused == "ref" && strings.HasPrefix(path, "template "):
		wantTemplates = true
	case focused == "ref" && strings.HasPrefix(path, "lock "):
		wantTemplates = false
	default:
		return bot.RespondAutocomplete(s, i, nil)
	}

	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.RespondAutocomplete(s, i, nil)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	service, _, err := f.serviceFor(fetchCtx, userID)
	if err != nil {
		return bot.RespondAutocomplete(s, i, nil)
	}

	var choices []*discordgo.ApplicationCommandOptionChoice
	if wantTemplates {
		templates, err := service.Templates(fetchCtx)
		if err != nil {
			return bot.RespondAutocomplete(s, i, nil)
		}
		sort.SliceStable(templates, func(left, right int) bool {
			if templates[left].Lockees != templates[right].Lockees {
				return templates[left].Lockees > templates[right].Lockees
			}
			return strings.ToLower(templates[left].LockName) < strings.ToLower(templates[right].LockName)
		})
		for _, template := range templates {
			label := strings.TrimSpace(template.LockName)
			if label == "" {
				label = "Untitled lock"
			}
			if !autocompleteMatches(current, label, template.ID.String()) {
				continue
			}
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  truncateChoice(label + " (" + template.ID.String() + ")"),
				Value: template.ID.String(),
			})
			if len(choices) >= 25 {
				break
			}
		}
	} else {
		views, err := service.Locks(fetchCtx, cs.LockFilter{Scope: cs.ScopeAll})
		if err != nil {
			return bot.RespondAutocomplete(s, i, nil)
		}
		for _, view := range views {
			label := strings.TrimSpace(view.Lock.LockName)
			if label == "" {
				label = "lock"
			}
			lockee := strings.TrimSpace(view.Lock.Lockee)
			if lockee != "" {
				label += " for " + lockee
			}
			if !autocompleteMatches(current, label, view.Lock.ID.String()) {
				continue
			}
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  truncateChoice(label + " (" + view.Lock.ID.String() + ")"),
				Value: view.Lock.ID.String(),
			})
			if len(choices) >= 25 {
				break
			}
		}
	}
	return bot.RespondAutocomplete(s, i, choices)
}

func focusedOption(i *discordgo.InteractionCreate) (string, string) {
	opts := subcommandOptions(i)
	for _, opt := range opts {
		if opt.Focused {
			return opt.Name, strings.TrimSpace(opt.StringValue())
		}
	}
	return "", ""
}

func autocompleteMatches(current, label, value string) bool {
	current = strings.ToLower(strings.TrimSpace(current))
	if current == "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(label), current) ||
		strings.HasPrefix(strings.ToLower(value), current)
}

func truncateChoice(value string) string {
	const limit = 100
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
