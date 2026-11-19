package bdsmlr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	"grimoire/internal/store"
)

const (
	// accountBase is the WordPress account site that hosts invitations.
	accountBase = "https://account.bdsmlr.com"

	// accountUserAgent is the browser identity used by the bbdsmlr app for
	// its account-site requests. The site rejects generic agents.
	accountUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

	requestTimeout = 20 * time.Second
	maxPageBytes   = 2 << 20
	maxRedirects   = 4

	componentNamespace = "bdsmlr"
	componentPrefix    = componentNamespace + ":"
	confirmationTTL    = 15 * time.Minute
	inviteWindow       = 24 * time.Hour
)

// accountBaseURL is a var so tests can point every request at a local server.
var accountBaseURL = accountBase

// errAccountSession means the configured cookie is missing, incomplete or no
// longer accepted by the account site. The operator has to refresh
// BDSMLR_SESSION.
var errAccountSession = errors.New("the BDSMLR account site requires a fresh session cookie")

// Options configures the bdsmlr feature.
type Options struct {
	GuildID string
	Session string
	Store   *store.Store
	Log     *slog.Logger
}

// Feature implements the BDSMLR invite command.
type Feature struct {
	guildID   string
	log       *slog.Logger
	store     *store.Store
	cookie    string
	userLogin string
	client    *http.Client

	// inviteMu serializes the rate-limit check and the send, so two button
	// presses cannot both pass the daily limit.
	inviteMu sync.Mutex
}

// New builds the bdsmlr feature.
func New(opts Options) *Feature {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	cookie := normalizeAccountCookie(opts.Session)
	userLogin, _ := userLoginFromCookie(cookie)
	return &Feature{
		guildID:   opts.GuildID,
		log:       opts.Log,
		store:     opts.Store,
		cookie:    cookie,
		userLogin: userLogin,
		client: &http.Client{
			Timeout: requestTimeout,
			// Redirects are followed by hand so a login redirect can be told
			// apart from the post-submit result redirect.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Register wires the feature into the bot. The command is open to members who
// hold the role configured with /settings command-role.
func (f *Feature) Register(b *bot.Bot) {
	if f.cookie == "" {
		f.log.Warn("bdsmlr invites have no usable BDSMLR_SESSION; the command will explain how to set it")
	} else if f.userLogin == "" {
		f.log.Warn("bdsmlr invites have no wordpress_logged_in_ cookie in BDSMLR_SESSION; the account username cannot be read")
	}
	b.AddUserCommand(command(), f.handleCommand)
	b.AddComponent(componentNamespace, f.handleComponent)
}

// handleCommand validates the address and asks for confirmation. The invite is
// only sent, and the daily limit only consumed, when the member presses the
// button.
func (f *Feature) handleCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return bot.ReplyEphemeral(s, i, "Use `/bdsmlr invite email:<address>`.")
	}

	sub := data.Options[0]
	if sub.Name != "invite" {
		return bot.ReplyEphemeral(s, i, "Unknown subcommand.")
	}

	rawEmail := ""
	for _, opt := range sub.Options {
		if opt.Name == "email" {
			rawEmail = opt.StringValue()
		}
	}
	email, err := validateEmail(rawEmail)
	if err != nil {
		return bot.ReplyEphemeral(s, i, err.Error())
	}

	userID := bot.InteractionUserID(i)
	if userID == "" {
		return bot.ReplyEphemeral(s, i, "I could not identify you. Try again in a moment.")
	}

	// Server admins are not subject to the member invite limit.
	admin := bot.CanUseBot(i)
	if !admin {
		allowed, next, err := f.inviteAllowed(ctx, userID)
		if err != nil {
			f.log.Error("checking bdsmlr invite limit", "user_id", userID, "err", err)
			return bot.ReplyEphemeral(s, i, "I could not check the invite limit. Try again in a moment.")
		}
		if !allowed {
			return bot.ReplyEphemeral(s, i, rateLimitMessage(next))
		}
	}

	if err := f.store.PruneBDSMLRConfirmations(ctx); err != nil {
		f.log.Warn("pruning bdsmlr confirmations", "err", err)
	}
	// Only the newest confirmation for this member stays usable.
	if err := f.store.DeleteBDSMLRConfirmationsForUser(ctx, f.guildID, userID); err != nil {
		return fmt.Errorf("clearing old bdsmlr confirmations: %w", err)
	}

	token, err := newConfirmationToken()
	if err != nil {
		return err
	}
	confirmation := &store.BDSMLRConfirmation{
		Token:     token,
		GuildID:   f.guildID,
		ChannelID: i.ChannelID,
		UserID:    userID,
		Email:     email,
		ExpiresAt: time.Now().UTC().Add(confirmationTTL),
	}
	if err := f.store.CreateBDSMLRConfirmation(ctx, confirmation); err != nil {
		return fmt.Errorf("saving the bdsmlr confirmation: %w", err)
	}

	f.log.Info("bdsmlr invite confirmation created", "user_id", userID)
	return bot.ReplyEphemeralWithComponents(s, i, confirmationMessage(email, admin), confirmationComponents(token))
}

// handleComponent serves the Send invite and Cancel buttons.
func (f *Feature) handleComponent(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	action, token, ok := parseComponentID(i.MessageComponentData().CustomID)
	if !ok || token == "" {
		return replaceConfirmation(s, i, "That button is no longer valid.", []discordgo.MessageComponent{})
	}

	confirmation, err := f.store.GetBDSMLRConfirmation(ctx, token)
	if err != nil {
		return fmt.Errorf("loading the bdsmlr confirmation: %w", err)
	}
	if confirmation == nil {
		return followupEphemeral(s, i, "That confirmation is no longer valid. Run `/bdsmlr invite` again.")
	}
	if time.Now().UTC().After(confirmation.ExpiresAt) {
		if err := f.store.DeleteBDSMLRConfirmation(ctx, token); err != nil {
			f.log.Warn("deleting an expired bdsmlr confirmation", "token", token, "err", err)
		}
		return replaceConfirmation(s, i, "That confirmation expired. Run `/bdsmlr invite` again.", []discordgo.MessageComponent{})
	}
	if confirmation.UserID != bot.InteractionUserID(i) || confirmation.GuildID != f.guildID {
		return replaceConfirmation(s, i, "That confirmation belongs to another member.", []discordgo.MessageComponent{})
	}

	switch action {
	case "cancel":
		claimed, err := f.store.ClaimBDSMLRConfirmation(ctx, token)
		if err != nil {
			return fmt.Errorf("canceling the bdsmlr confirmation: %w", err)
		}
		if !claimed {
			return alreadyHandled(s, i)
		}
		return replaceConfirmation(s, i, "Confirmation canceled. No invitation was sent.", []discordgo.MessageComponent{})
	case "send":
		return f.confirmInvite(ctx, s, i, confirmation)
	default:
		return replaceConfirmation(s, i, "That button is no longer valid.", []discordgo.MessageComponent{})
	}
}

// confirmInvite re-checks the daily limit, sends the invitation, and records
// it only when the account site accepted it.
func (f *Feature) confirmInvite(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, confirmation *store.BDSMLRConfirmation) error {
	// The site flow can take longer than the interaction deadline.
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()

	f.inviteMu.Lock()
	defer f.inviteMu.Unlock()

	// Claim the token before doing anything else. A double-click or a Send and
	// Cancel race then has exactly one winner.
	claimed, err := f.store.ClaimBDSMLRConfirmation(workCtx, confirmation.Token)
	if err != nil {
		f.log.Error("claiming bdsmlr confirmation", "token", confirmation.Token, "err", err)
		return bot.EditEphemeralWithComponents(s, i,
			"I could not process the invite. Try again in a moment.",
			confirmationComponents(confirmation.Token),
		)
	}
	if !claimed {
		return alreadyHandled(s, i)
	}

	// Server admins are not subject to the member invite limit.
	admin := bot.CanUseBot(i)
	if !admin {
		allowed, next, err := f.inviteAllowed(workCtx, confirmation.UserID)
		if err != nil {
			f.log.Error("checking bdsmlr invite limit", "user_id", confirmation.UserID, "err", err)
			f.restoreConfirmation(workCtx, confirmation)
			return bot.EditEphemeralWithComponents(s, i,
				"I could not check the invite limit. Try again in a moment.",
				confirmationComponents(confirmation.Token),
			)
		}
		if !allowed {
			return bot.EditEphemeralWithComponents(s, i, rateLimitMessage(next), []discordgo.MessageComponent{})
		}
	}

	message, err := f.sendInvite(workCtx, confirmation.Email)
	if err != nil {
		// The claim deleted the pending row. Restore it so a definite failure,
		// such as a missing session or a rejected form, can be retried. A
		// duplicate attempt to the same address is rejected by the site.
		f.restoreConfirmation(workCtx, confirmation)
		retry := confirmationComponents(confirmation.Token)
		if errors.Is(err, errAccountSession) {
			f.log.Warn("bdsmlr invite needs a session", "err", err)
			return bot.EditEphemeralWithComponents(s, i,
				"The bdsmlr session is missing or expired. An admin needs to set `BDSMLR_SESSION` to a fresh cookie header copied from a signed-in account.bdsmlr.com session.\n\nYou can try again with the button below.",
				retry,
			)
		}
		f.log.Warn("bdsmlr invite failed", "email", confirmation.Email, "err", err)
		return bot.EditEphemeralWithComponents(s, i,
			"I could not send the invite: "+err.Error()+"\n\nYou can try again with the button below.",
			retry,
		)
	}

	if !admin {
		if err := f.store.RecordBDSMLRInvite(workCtx, f.guildID, confirmation.UserID); err != nil {
			f.log.Error("recording bdsmlr invite", "user_id", confirmation.UserID, "err", err)
		}
	}

	// Public announcement in the channel where the command was used, so the
	// server can see that the bot is being used. The invited email stays out
	// of it.
	if confirmation.ChannelID != "" {
		if _, err := s.ChannelMessageSend(confirmation.ChannelID, publicInviteMessage(confirmation.UserID)); err != nil {
			f.log.Warn("posting the bdsmlr invite announcement",
				"channel_id", confirmation.ChannelID,
				"user_id", confirmation.UserID,
				"err", err,
			)
		}
	}

	f.log.Info("bdsmlr invite sent",
		"user_id", confirmation.UserID,
		"email", confirmation.Email,
		"message", message,
	)
	return bot.EditEphemeralWithComponents(s, i,
		fmt.Sprintf("Invitation sent to **%s**. bdsmlr will email them a link to join.", confirmation.Email),
		[]discordgo.MessageComponent{},
	)
}

// inviteAllowed reports whether the member may send an invite now. next is the
// time the limit lifts when they may not.
func (f *Feature) inviteAllowed(ctx context.Context, userID string) (bool, time.Time, error) {
	last, err := f.store.LastBDSMLRInvite(ctx, f.guildID, userID)
	if err != nil {
		return false, time.Time{}, err
	}
	if last.IsZero() {
		return true, time.Time{}, nil
	}
	next := last.Add(inviteWindow)
	if time.Now().UTC().Before(next) {
		return false, next, nil
	}
	return true, time.Time{}, nil
}

func replaceConfirmation(s *discordgo.Session, i *discordgo.InteractionCreate, content string, components []discordgo.MessageComponent) error {
	return bot.EditEphemeralWithComponents(s, i, content, components)
}

// alreadyHandled answers a second click with a separate ephemeral message, so
// it does not overwrite the result of the interaction that won the claim.
func alreadyHandled(s *discordgo.Session, i *discordgo.InteractionCreate) error {
	return followupEphemeral(s, i, "This invite was already handled.")
}

func followupEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) error {
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: content,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	return err
}

// restoreConfirmation puts a claimed confirmation back after a failure, so the
// retry button still works.
func (f *Feature) restoreConfirmation(ctx context.Context, confirmation *store.BDSMLRConfirmation) {
	if err := f.store.CreateBDSMLRConfirmation(ctx, confirmation); err != nil {
		f.log.Error("restoring bdsmlr confirmation", "token", confirmation.Token, "err", err)
	}
}

func confirmationMessage(email string, admin bool) string {
	if admin {
		return fmt.Sprintf(
			"**Send an invite?**\n\nEmail: `%s`\n\nDouble-check the address before confirming. bdsmlr will email this address an invitation link, and the invite can only be accepted by that address.\n\nPress **Send invite** to confirm. This confirmation expires in %d minutes.",
			email,
			int(confirmationTTL.Minutes()),
		)
	}
	return fmt.Sprintf(
		"**Send an invite?**\n\nEmail: `%s`\n\nDouble-check the address before confirming. bdsmlr will email this address an invitation link, and the invite can only be accepted by that address.\n\nPress **Send invite** to confirm. You can send one invite every 24 hours, and this confirmation expires in %d minutes.",
		email,
		int(confirmationTTL.Minutes()),
	)
}

func confirmationComponents(token string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Send invite",
				Style:    discordgo.PrimaryButton,
				CustomID: componentPrefix + "send:" + token,
			},
			discordgo.Button{
				Label:    "Cancel",
				Style:    discordgo.SecondaryButton,
				CustomID: componentPrefix + "cancel:" + token,
			},
		}},
	}
}

func rateLimitMessage(next time.Time) string {
	return fmt.Sprintf(
		"You can send one invite every 24 hours. Your last invite was sent recently, so you can send another <t:%d:R>.",
		next.Unix(),
	)
}

func publicInviteMessage(userID string) string {
	return fmt.Sprintf("An invite has been sent to your email <@%s>", userID)
}

// parseComponentID splits bdsmlr:send:<token> and bdsmlr:cancel:<token>.
func parseComponentID(id string) (action, token string, ok bool) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 || parts[0] != componentNamespace {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func newConfirmationToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generating a bdsmlr confirmation token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
