package chastisafe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	cs "github.com/zastraeaxyz/chastisafe-go"
	"grimoire/internal/store"
)

const modalNamespace = "chastisafe"

// handleCSCommand routes every /cs subcommand.
func (f *Feature) handleCSCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	path := subcommandPath(i)
	opts := subcommandOptions(i)

	switch path {
	case "login":
		return f.handleLoginCommand(ctx, s, i)
	case "logout":
		return f.handleLogoutCommand(ctx, s, i)
	case "account":
		return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
			profile, err := service.Profile(ctx)
			if err != nil {
				identity, identityErr := service.Identity(ctx)
				if identityErr != nil {
					return "", err
				}
				return fmt.Sprintf("ChastiSafe: **%s** (ID `%s`)", identity.User, identity.ID), nil
			}
			return cs.ProfileText(profile), nil
		})
	case "lookup":
		target, ok := stringOption(opts, "user")
		if !ok {
			return bot.ReplyEphemeral(s, i, "Give me a ChastiSafe user ID or username#number.")
		}
		return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
			profile, err := service.ProfileFor(ctx, target)
			if err != nil {
				return "", err
			}
			return cs.ProfileText(profile), nil
		})
	case "status":
		return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, cred *store.ChastiSafeCredential) (string, error) {
			locks, err := service.Loaded(ctx)
			if err != nil {
				return "", err
			}
			return cs.PersonalStatusText(locks, cred.ChastiSafeID), nil
		})
	case "lockees":
		return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
			locks, err := service.Lockees(ctx)
			if err != nil {
				return "", err
			}
			return cs.LockeesText(locks), nil
		})
	case "templates":
		return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
			templates, err := service.Templates(ctx)
			if err != nil {
				return "", err
			}
			return cs.LocksText(templates), nil
		})
	case "template view":
		return f.handleTemplateView(ctx, s, i, opts)
	case "template create":
		return f.handleTemplateCreate(ctx, s, i, opts)
	case "template clone":
		return f.handleTemplateClone(ctx, s, i, opts)
	case "template update":
		return f.handleTemplateUpdate(ctx, s, i, opts)
	case "template delete":
		return f.handleTemplateDelete(ctx, s, i, opts)
	case "template simulate":
		return f.handleTemplateSimulate(ctx, s, i, opts)
	case "template card":
		return f.handleTemplateCard(ctx, s, i, opts)
	case "template background":
		return f.handleTemplateBackground(ctx, s, i, opts)
	case "locks":
		return f.handleLocks(ctx, s, i, opts)
	case "lock view":
		return f.handleLockView(ctx, s, i, opts)
	case "lock load":
		return f.handleLockLoad(ctx, s, i, opts)
	case "lock addtime":
		return f.handleLockAddTime(ctx, s, i, opts)
	case "lock settime":
		return f.handleLockSetTime(ctx, s, i, opts)
	case "lock freeze":
		return f.handleLockFreeze(ctx, s, i, opts)
	case "lock hide":
		return f.handleLockHide(ctx, s, i, opts)
	case "lock purpose":
		return f.handleLockPurpose(ctx, s, i, opts)
	case "lock cards":
		return f.handleLockCards(ctx, s, i, opts)
	case "lock settings":
		return f.handleLockSettings(ctx, s, i, opts)
	case "lock unlock":
		return f.handleLockUnlock(ctx, s, i, opts)
	case "lock reset":
		return f.handleLockReset(ctx, s, i, opts)
	case "lock abandon":
		return f.handleLockAbandon(ctx, s, i, opts)
	case "lock split":
		return f.handleLockSplit(ctx, s, i, opts)
	default:
		return bot.ReplyEphemeral(s, i, "Unknown `/cs` subcommand.")
	}
}

func (f *Feature) runWithService(
	ctx context.Context,
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	fn func(context.Context, *cs.Service, *store.ChastiSafeCredential) (string, error),
) error {
	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	service, cred, err := f.serviceFor(ctx, userID)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}
	text, err := fn(ctx, service, cred)
	if err != nil {
		return bot.EditEphemeral(s, i, f.apiFailure(ctx, userID, err))
	}
	return sendEphemeralChunks(s, i, text)
}

func (f *Feature) handleLoginCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if f.store == nil || f.box == nil {
		return bot.ReplyEphemeral(s, i, "ChastiSafe login is not configured on this bot.")
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			Title:    "ChastiSafe login",
			CustomID: modalNamespace + ":login",
			Components: []discordgo.MessageComponent{
				&discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						&discordgo.TextInput{
							CustomID:    "code",
							Label:       "Developer code",
							Style:       discordgo.TextInputShort,
							Placeholder: "Paste the code from your ChastiSafe profile",
							MinLength:   1,
							MaxLength:   512,
							Required:    true,
						},
					},
				},
			},
		},
	})
}

func (f *Feature) handleLoginModal(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}
	if !f.loginAttemptAllowed(userID) {
		return bot.ReplyEphemeral(s, i, "Too many login attempts. Wait a few minutes and try again.")
	}

	code := modalValue(i.ModalSubmitData().Components, "code")
	if strings.TrimSpace(code) == "" {
		return bot.ReplyEphemeral(s, i, "The developer code was empty. Run `/cs login` again.")
	}

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	identity, err := f.authenticate(ctx, userID, code)
	if err != nil {
		f.log.Info("chastisafe login failed", "user_id", userID, "err", err)
		switch {
		case errors.Is(err, errNotOwner):
			return bot.EditEphemeral(s, i,
				"That code belongs to a different ChastiSafe account.\n"+
					"Link your Discord account in ChastiSafe, then run `/cs login` again.")
		case errors.Is(err, cs.ErrUnauthorized):
			return bot.EditEphemeral(s, i, "ChastiSafe rejected that developer code. Check it and try again.")
		case errors.Is(err, errNotConfigured):
			return bot.EditEphemeral(s, i, "ChastiSafe login is not configured on this bot.")
		default:
			f.log.Warn("chastisafe login request failed", "user_id", userID, "err", err)
			return bot.EditEphemeral(s, i, "I could not reach ChastiSafe right now. Try again in a moment.")
		}
	}

	f.loginSucceeded(userID)
	f.log.Info("chastisafe login stored",
		"user_id", userID,
		"chastisafe_user", identity.User,
		"chastisafe_id", identity.ID.String(),
	)
	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"Logged in as **%s** (ID `%s`).\n"+
			"Use `/cs status`, `/cs templates`, `/cs locks`, and `/cs lookup`.",
		identity.User, identity.ID,
	))
}

func (f *Feature) handleLogoutCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I cannot tell who you are.")
	}
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	if err := f.forget(ctx, userID); err != nil {
		if errors.Is(err, errNotConfigured) {
			return bot.EditEphemeral(s, i, "ChastiSafe login is not configured on this bot.")
		}
		return err
	}
	f.log.Info("chastisafe logout", "user_id", userID)
	return bot.EditEphemeral(s, i, "Your saved ChastiSafe code has been removed.")
}

func (f *Feature) handleTemplateView(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		template, err := service.TemplateForEdit(ctx, ref)
		if err != nil {
			return "", err
		}
		return cs.TemplateDetailText(template), nil
	})
}

func (f *Feature) handleTemplateCreate(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	draft, err := templateDraftFromOptions(opts)
	if err != nil {
		return bot.ReplyEphemeral(s, i, userError(err))
	}
	if _, err := draft.Payload(); err != nil {
		return bot.ReplyEphemeral(s, i, userError(err))
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		created, err := service.CreateTemplate(ctx, draft)
		if errors.Is(err, cs.ErrTemplateNotListed) {
			return cs.TemplateDetailText(created) +
				"\n\nChastiSafe accepted the template but has not listed it yet. Run `/cs templates` in a moment.", nil
		}
		if err != nil {
			return "", err
		}
		return cs.TemplateDetailText(created), nil
	})
}

func (f *Feature) handleTemplateClone(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	name, _ := stringOption(opts, "name")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		created, err := service.CloneTemplate(ctx, ref, name)
		if errors.Is(err, cs.ErrTemplateNotListed) {
			return cs.TemplateDetailText(created) +
				"\n\nChastiSafe accepted the copy but has not listed it yet. Run `/cs templates` in a moment.", nil
		}
		if err != nil {
			return "", err
		}
		return "Cloned into:\n" + cs.TemplateDetailText(created), nil
	})
}

func (f *Feature) handleTemplateUpdate(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	patch, err := templatePatchFromOptions(opts)
	if err != nil {
		return bot.ReplyEphemeral(s, i, userError(err))
	}
	if _, err := patch.Payload("validation"); err != nil {
		return bot.ReplyEphemeral(s, i, userError(err))
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.UpdateTemplate(ctx, ref, patch)
		if err != nil {
			return "", err
		}
		return "Updated:\n" + cs.TemplateDetailText(updated), nil
	})
}

func (f *Feature) handleTemplateDelete(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	if !confirm(opts) {
		return bot.ReplyEphemeral(s, i, "Add `confirm:true` to delete a template.")
	}
	ref, _ := stringOption(opts, "ref")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		template, err := service.ResolveTemplate(ctx, ref)
		if err != nil {
			return "", err
		}
		if err := service.DeleteTemplate(ctx, template.ID.String()); err != nil {
			return "", err
		}
		name := strings.TrimSpace(template.LockName)
		if name == "" {
			name = template.ID.String()
		}
		return fmt.Sprintf("Deleted **%s**.", name), nil
	})
}

func (f *Feature) handleTemplateSimulate(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		sim, err := service.SimulateTemplate(ctx, ref)
		if err != nil {
			return "", err
		}
		return simulationText(sim), nil
	})
}

func (f *Feature) handleTemplateCard(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	cardName, _ := stringOption(opts, "card")
	card := cs.NormalizeCard(cardName)
	if !card.Valid() {
		return bot.ReplyEphemeral(s, i, "Unknown card.")
	}
	min, _ := intOption(opts, "min")
	max, _ := intOption(opts, "max")
	if min < 0 || max < 0 || min > max {
		return bot.ReplyEphemeral(s, i, "Card minimum and maximum must be non-negative and minimum cannot exceed maximum.")
	}
	change := cs.CardRange{Card: card, Min: min, Max: max}
	if value, ok := intOptionValue(opts, "greens_required"); ok {
		if value < 0 {
			return bot.ReplyEphemeral(s, i, "Greens required cannot be negative.")
		}
		change.GreensRequired = &value
	}
	if value, ok := intOptionValue(opts, "dice_count"); ok {
		if value < 1 {
			return bot.ReplyEphemeral(s, i, "Dice count must be at least 1.")
		}
		change.DiceCount = &value
	}
	if value, ok := intOptionValue(opts, "dice_faces"); ok {
		if value < 1 {
			return bot.ReplyEphemeral(s, i, "Dice faces must be at least 1.")
		}
		change.DiceFaces = &value
	}
	if value, ok := stringOption(opts, "pink_script"); ok {
		change.PinkScript = &value
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		if err := service.UpdateTemplateCard(ctx, ref, change); err != nil {
			return "", err
		}
		template, err := service.TemplateForEdit(ctx, ref)
		if err != nil {
			return fmt.Sprintf("Updated card range %s on **%s**.", card.Short(), ref), nil
		}
		return "Updated:\n" + cs.TemplateDetailText(template), nil
	})
}

func (f *Feature) handleTemplateBackground(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	background, _ := stringOption(opts, "background")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		if err := service.SetTemplateBackground(ctx, ref, background); err != nil {
			return "", err
		}
		return fmt.Sprintf("Background updated to `%s`.", background), nil
	})
}

func (f *Feature) handleLocks(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	scope := cs.ScopeAll
	if value, ok := stringOption(opts, "scope"); ok {
		scope = cs.LockScope(value)
	}
	var state cs.LockState
	if value, ok := stringOption(opts, "state"); ok {
		state = cs.LockState(value)
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		views, err := service.Locks(ctx, cs.LockFilter{Scope: scope, State: state})
		if err != nil {
			return "", err
		}
		return cs.LockListText(views), nil
	})
}

func (f *Feature) handleLockView(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		view, err := service.Lock(ctx, ref)
		if err != nil {
			return "", err
		}
		return cs.LockDetailText(view), nil
	})
}

func (f *Feature) handleLockLoad(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	template, _ := stringOption(opts, "template")
	code, codeOK := stringOption(opts, "code")
	if !codeOK {
		return bot.ReplyEphemeral(s, i, "Give me the lock code you want to set.")
	}
	purpose, err := purposeFromOptions(opts)
	if err != nil {
		return bot.ReplyEphemeral(s, i, err.Error())
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		locks, err := service.LoadLock(ctx, template, code, purpose)
		if err != nil {
			return "", err
		}
		if len(locks) == 0 {
			return "ChastiSafe did not return a loaded lock.", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Loaded **%d** lock%s.\n", len(locks), cs.PluralSuffix(len(locks)))
		for index := range locks {
			fmt.Fprintf(&b, "\n• %s", cs.LockSummary(&locks[index]))
		}
		return b.String(), nil
	})
}

func (f *Feature) handleLockAddTime(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	amount, ok, err := durationOption(opts, "amount")
	if err != nil || !ok {
		return bot.ReplyEphemeral(s, i, durationError(err, "amount"))
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.AddLockTime(ctx, ref, amount)
		if err != nil {
			return "", err
		}
		return f.updatedLockText(ctx, service, updated), nil
	})
}

func (f *Feature) handleLockSetTime(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	amount, ok, err := durationOption(opts, "amount")
	if err != nil || !ok {
		return bot.ReplyEphemeral(s, i, durationError(err, "amount"))
	}
	if amount < 0 {
		return bot.ReplyEphemeral(s, i, "Set time cannot be negative. Use `/cs lock addtime` to remove time.")
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.SetLockTime(ctx, ref, amount)
		if err != nil {
			return "", err
		}
		return f.updatedLockText(ctx, service, updated), nil
	})
}

func (f *Feature) handleLockFreeze(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	frozen, _ := boolOptionValue(opts, "frozen")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.FreezeLock(ctx, ref, frozen)
		if err != nil {
			return "", err
		}
		return f.updatedLockText(ctx, service, updated), nil
	})
}

func (f *Feature) handleLockHide(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	hidden, _ := boolOptionValue(opts, "hidden")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.HideLock(ctx, ref, hidden)
		if err != nil {
			return "", err
		}
		return f.updatedLockText(ctx, service, updated), nil
	})
}

func (f *Feature) handleLockPurpose(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	purpose, err := purposeFromOptions(opts)
	if err != nil {
		return bot.ReplyEphemeral(s, i, err.Error())
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.SetLockPurpose(ctx, ref, purpose)
		if err != nil {
			return "", err
		}
		return f.updatedLockText(ctx, service, updated), nil
	})
}

func (f *Feature) handleLockCards(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	addRaw, _ := stringOption(opts, "add")
	removeRaw, _ := stringOption(opts, "remove")
	if removeRaw != "" && !confirm(opts) {
		return bot.ReplyEphemeral(s, i, "Removing cards is destructive. Add `confirm:true`.")
	}
	changes := make([]cs.CardChange, 0, 4)
	if addRaw != "" {
		cards, err := cs.ParseCards(addRaw)
		if err != nil {
			return bot.ReplyEphemeral(s, i, err.Error())
		}
		for card, count := range cards {
			changes = append(changes, cs.CardChange{Card: card, Delta: count})
		}
	}
	if removeRaw != "" {
		cards, err := cs.ParseCards(removeRaw)
		if err != nil {
			return bot.ReplyEphemeral(s, i, err.Error())
		}
		for card, count := range cards {
			changes = append(changes, cs.CardChange{Card: card, Delta: -count})
		}
	}
	if len(changes) == 0 {
		return bot.ReplyEphemeral(s, i, "Give me cards to add or remove.")
	}
	message, _ := stringOption(opts, "message")
	show, _ := boolOptionValue(opts, "show")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.UpdateLockCards(ctx, ref, changes, cs.UpdateOptions{Message: message, Show: show})
		if err != nil {
			return "", err
		}
		return f.updatedLockText(ctx, service, updated), nil
	})
}

func (f *Feature) handleLockSettings(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	ref, _ := stringOption(opts, "ref")
	patch, err := lockSettingsPatchFromOptions(opts)
	if err != nil {
		return bot.ReplyEphemeral(s, i, userError(err))
	}
	if err := patch.Validate(); err != nil {
		return bot.ReplyEphemeral(s, i, userError(err))
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		updated, err := service.UpdateLockSettings(ctx, ref, patch)
		if err != nil {
			return "", err
		}
		return f.updatedLockText(ctx, service, updated), nil
	})
}

func (f *Feature) handleLockUnlock(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	if !confirm(opts) {
		return bot.ReplyEphemeral(s, i, "Add `confirm:true` to unlock a lock.")
	}
	ref, _ := stringOption(opts, "ref")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		view, err := service.Lock(ctx, ref)
		if err != nil {
			return "", err
		}
		if _, err := service.UnlockLock(ctx, view.Lock.ID.String()); err != nil {
			return "", err
		}
		return fmt.Sprintf("Unlock requested for **%s**. The code is not shown here.", lockName(view.Lock.LockName)), nil
	})
}

func (f *Feature) handleLockReset(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	if !confirm(opts) {
		return bot.ReplyEphemeral(s, i, "Add `confirm:true` to reset a lock.")
	}
	ref, _ := stringOption(opts, "ref")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		view, err := service.Lock(ctx, ref)
		if err != nil {
			return "", err
		}
		if _, err := service.ResetLock(ctx, view.Lock.ID.String()); err != nil {
			return "", err
		}
		return fmt.Sprintf("Reset requested for **%s**. The code is not shown here.", lockName(view.Lock.LockName)), nil
	})
}

func (f *Feature) handleLockAbandon(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	if !confirm(opts) {
		return bot.ReplyEphemeral(s, i, "Add `confirm:true` to abandon a lock.")
	}
	ref, _ := stringOption(opts, "ref")
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		view, err := service.Lock(ctx, ref)
		if err != nil {
			return "", err
		}
		if err := service.AbandonLock(ctx, view.Lock.ID.String()); err != nil {
			return "", err
		}
		return fmt.Sprintf("Ended **%s**.", lockName(view.Lock.LockName)), nil
	})
}

func (f *Feature) handleLockSplit(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	if !confirm(opts) {
		return bot.ReplyEphemeral(s, i, "Add `confirm:true` to split a lock.")
	}
	ref, _ := stringOption(opts, "ref")
	count, _ := intOption(opts, "count")
	if count < 2 {
		return bot.ReplyEphemeral(s, i, "Split count must be at least 2.")
	}
	return f.runWithService(ctx, s, i, func(ctx context.Context, service *cs.Service, _ *store.ChastiSafeCredential) (string, error) {
		locks, err := service.SplitLock(ctx, ref, count)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Created **%d** fake lock%s.", len(locks), cs.PluralSuffix(len(locks))), nil
	})
}

func (f *Feature) updatedLockText(ctx context.Context, service *cs.Service, updated *cs.LoadedLock) string {
	if updated == nil {
		return "Updated."
	}
	view, err := service.Lock(ctx, updated.ID.String())
	if err != nil {
		return cs.LockDetailText(&cs.LockView{Lock: *updated})
	}
	return cs.LockDetailText(view)
}

func simulationText(sim *cs.Simulation) string {
	if sim == nil {
		return "ChastiSafe returned no simulation."
	}
	var b strings.Builder
	b.WriteString("**Simulation**\n")
	fmt.Fprintf(&b, "Duration: average %s, range %s to %s\n",
		cs.FormatShortDuration(time.Duration(sim.AvgDuration)*time.Second),
		cs.FormatShortDuration(time.Duration(sim.MinDuration)*time.Second),
		cs.FormatShortDuration(time.Duration(sim.MaxDuration)*time.Second),
	)
	fmt.Fprintf(&b, "Cards: average %d, range %d to %d\n", sim.AvgCards, sim.MinCards, sim.MaxCards)
	fmt.Fprintf(&b, "Regularity: %s\n", cs.FormatMinutes(sim.Regularity))
	return strings.TrimRight(b.String(), "\n")
}

func templateDraftFromOptions(opts []*discordgo.ApplicationCommandInteractionDataOption) (cs.TemplateDraft, error) {
	name, _ := stringOption(opts, "name")
	typeRaw, _ := stringOption(opts, "type")
	templateType, err := cs.ParseTemplateType(typeRaw)
	if err != nil {
		return cs.TemplateDraft{}, err
	}
	draft := cs.TemplateDraft{Name: name, Type: templateType}
	if value, ok := stringOption(opts, "description"); ok {
		draft.Description = value
	}
	if value, ok, err := durationOption(opts, "timer_min"); err != nil {
		return cs.TemplateDraft{}, err
	} else if ok {
		draft.TimerMin = value
	}
	if value, ok, err := durationOption(opts, "timer_max"); err != nil {
		return cs.TemplateDraft{}, err
	} else if ok {
		draft.TimerMax = value
	}
	if value, ok, err := durationOption(opts, "regularity"); err != nil {
		return cs.TemplateDraft{}, err
	} else if ok {
		draft.Regularity = value
	}
	if value, ok, err := durationOption(opts, "minimum_duration"); err != nil {
		return cs.TemplateDraft{}, err
	} else if ok {
		draft.MinimumDuration = value
	}
	if value, ok := stringOption(opts, "cards"); ok {
		cards, err := cs.ParseCards(value)
		if err != nil {
			return cs.TemplateDraft{}, err
		}
		for card, count := range cards {
			draft.Cards = append(draft.Cards, cs.TemplateCard{Name: card, Min: count, Max: count})
		}
	}
	if value, ok := stringOption(opts, "allowed_purposes"); ok {
		mask, err := cs.ParsePurposes(value)
		if err != nil {
			return cs.TemplateDraft{}, err
		}
		draft.AllowedPurposes = mask
	}
	if value, ok := boolOptionValue(opts, "hidden"); ok {
		draft.Hidden = value
	}
	if value, ok := boolOptionValue(opts, "start_frozen"); ok {
		draft.StartFrozen = value
	}
	if value, ok := boolOptionValue(opts, "allow_updates"); ok {
		draft.AllowUpdates = &value
	}
	if value, ok := boolOptionValue(opts, "cumulative"); ok {
		draft.Cumulative = &value
	}
	return draft, nil
}

func templatePatchFromOptions(opts []*discordgo.ApplicationCommandInteractionDataOption) (cs.TemplatePatch, error) {
	var patch cs.TemplatePatch
	if value, ok := stringOption(opts, "name"); ok {
		patch.Name = &value
	}
	if value, ok := stringOption(opts, "description"); ok {
		patch.Description = &value
	}
	if value, ok := boolOptionValue(opts, "hidden"); ok {
		patch.Hidden = &value
	}
	if value, ok := boolOptionValue(opts, "start_frozen"); ok {
		patch.StartFrozen = &value
	}
	if value, ok := boolOptionValue(opts, "allow_updates"); ok {
		patch.AllowUpdates = &value
	}
	if value, ok := boolOptionValue(opts, "cumulative"); ok {
		patch.Cumulative = &value
	}
	for name, target := range map[string]**time.Duration{
		"timer_min":         &patch.TimerMin,
		"timer_max":         &patch.TimerMax,
		"regularity":        &patch.Regularity,
		"checkin_period":    &patch.CheckInPeriod,
		"checkin_tolerance": &patch.CheckInTolerance,
		"hygiene_period":    &patch.HygieneUnlockPeriod,
		"hygiene_duration":  &patch.HygieneUnlockDuration,
		"autoreset_period":  &patch.AutoResetPeriod,
		"minimum_duration":  &patch.MinimumDuration,
	} {
		if value, ok, err := durationOption(opts, name); err != nil {
			return cs.TemplatePatch{}, err
		} else if ok {
			*target = &value
		}
	}
	if value, ok := intOptionValue(opts, "autoreset_count"); ok {
		patch.AutoResetCount = &value
	}
	if value, ok := stringOption(opts, "allowed_purposes"); ok {
		mask, err := cs.ParsePurposes(value)
		if err != nil {
			return cs.TemplatePatch{}, err
		}
		patch.AllowedPurposes = &mask
	}
	if value, ok := stringOption(opts, "status"); ok {
		patch.Status = &value
	}
	if patchEmpty(patch) {
		return cs.TemplatePatch{}, fmt.Errorf("chastisafe: give me at least one template field to update")
	}
	return patch, nil
}

func patchEmpty(patch cs.TemplatePatch) bool {
	return patch.Name == nil && patch.Description == nil && patch.Hidden == nil &&
		patch.StartFrozen == nil && patch.AllowUpdates == nil && patch.Cumulative == nil &&
		patch.TimerMin == nil && patch.TimerMax == nil && patch.Regularity == nil &&
		patch.CheckInPeriod == nil && patch.CheckInTolerance == nil &&
		patch.HygieneUnlockPeriod == nil && patch.HygieneUnlockDuration == nil &&
		patch.AutoResetPeriod == nil && patch.AutoResetCount == nil &&
		patch.MinimumDuration == nil && patch.AllowedPurposes == nil && patch.Status == nil
}

func lockSettingsPatchFromOptions(opts []*discordgo.ApplicationCommandInteractionDataOption) (cs.LockSettingsPatch, error) {
	var patch cs.LockSettingsPatch
	if value, ok := intOptionValue(opts, "max_picks"); ok {
		patch.MaxPicks = &value
	}
	if value, ok := intOptionValue(opts, "autoreset_count"); ok {
		patch.AutoResetCount = &value
	}
	for name, target := range map[string]**time.Duration{
		"regularity":        &patch.Regularity,
		"checkin_period":    &patch.CheckInPeriod,
		"checkin_tolerance": &patch.CheckInTolerance,
		"hygiene_period":    &patch.HygieneUnlockPeriod,
		"hygiene_duration":  &patch.HygieneUnlockDuration,
		"autoreset_period":  &patch.AutoResetPeriod,
		"remaining_time":    &patch.RemainingTime,
		"add_time":          &patch.AddTime,
	} {
		if value, ok, err := durationOption(opts, name); err != nil {
			return cs.LockSettingsPatch{}, err
		} else if ok {
			*target = &value
		}
	}
	if value, ok := stringOption(opts, "purpose"); ok {
		purpose, err := cs.ParsePurpose(value)
		if err != nil {
			return cs.LockSettingsPatch{}, err
		}
		patch.LockPurpose = &purpose
	}
	if patch.MaxPicks == nil && patch.AutoResetCount == nil && patch.Regularity == nil &&
		patch.CheckInPeriod == nil && patch.CheckInTolerance == nil &&
		patch.HygieneUnlockPeriod == nil && patch.HygieneUnlockDuration == nil &&
		patch.AutoResetPeriod == nil && patch.RemainingTime == nil &&
		patch.AddTime == nil && patch.LockPurpose == nil {
		return cs.LockSettingsPatch{}, fmt.Errorf("chastisafe: give me at least one lock setting to change")
	}
	return patch, nil
}

func purposeFromOptions(opts []*discordgo.ApplicationCommandInteractionDataOption) (cs.Purpose, error) {
	value, ok := stringOption(opts, "purpose")
	if !ok {
		return "", nil
	}
	return cs.ParsePurpose(value)
}

func durationError(err error, name string) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("I need a valid `%s` duration, for example `30m` or `2h`.", name)
}

func confirm(opts []*discordgo.ApplicationCommandInteractionDataOption) bool {
	value, _ := boolOptionValue(opts, "confirm")
	return value
}

func subcommandPath(i *discordgo.InteractionCreate) string {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return ""
	}
	first := data.Options[0]
	switch first.Type {
	case discordgo.ApplicationCommandOptionSubCommand:
		return first.Name
	case discordgo.ApplicationCommandOptionSubCommandGroup:
		if len(first.Options) == 0 {
			return first.Name
		}
		return first.Name + " " + first.Options[0].Name
	default:
		return ""
	}
}

func subcommandOptions(i *discordgo.InteractionCreate) []*discordgo.ApplicationCommandInteractionDataOption {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return nil
	}
	first := data.Options[0]
	switch first.Type {
	case discordgo.ApplicationCommandOptionSubCommand:
		return first.Options
	case discordgo.ApplicationCommandOptionSubCommandGroup:
		if len(first.Options) == 0 {
			return nil
		}
		return first.Options[0].Options
	default:
		return nil
	}
}

func stringOption(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (string, bool) {
	for _, opt := range opts {
		if opt.Name != name {
			continue
		}
		value := strings.TrimSpace(opt.StringValue())
		if value == "" {
			return "", false
		}
		return value, true
	}
	return "", false
}

func boolOptionValue(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (bool, bool) {
	for _, opt := range opts {
		if opt.Name == name {
			return opt.BoolValue(), true
		}
	}
	return false, false
}

func intOption(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (int, bool) {
	value, ok := intOptionValue(opts, name)
	return value, ok
}

func intOptionValue(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (int, bool) {
	for _, opt := range opts {
		if opt.Name == name {
			return int(opt.IntValue()), true
		}
	}
	return 0, false
}

func durationOption(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (time.Duration, bool, error) {
	value, ok := stringOption(opts, name)
	if !ok {
		return 0, false, nil
	}
	duration, err := cs.ParseDuration(value)
	if err != nil {
		return 0, false, err
	}
	return duration, true, nil
}

func modalValue(components []discordgo.MessageComponent, customID string) string {
	for _, component := range components {
		row, ok := component.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, child := range row.Components {
			input, ok := child.(*discordgo.TextInput)
			if ok && input.CustomID == customID {
				return input.Value
			}
		}
	}
	return ""
}

func lockName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "lock"
	}
	return value
}

func userError(err error) string {
	return strings.TrimPrefix(err.Error(), "chastisafe: ")
}
