package bdsmlr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
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
	Log     *slog.Logger
}

// Feature implements the BDSMLR invite command.
type Feature struct {
	guildID string
	log     *slog.Logger
	cookie  string
	client  *http.Client

	userMu    sync.Mutex
	userLogin string
}

// New builds the bdsmlr feature.
func New(opts Options) *Feature {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Feature{
		guildID: opts.GuildID,
		log:     opts.Log,
		cookie:  normalizeAccountCookie(opts.Session),
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
	}
	b.AddUserCommand(command(), f.handleCommand)
}

// handleCommand routes /bdsmlr subcommands.
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

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	// Several page loads and a redirect chain do not fit in the interaction
	// deadline the bot sets for handlers.
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()

	message, err := f.sendInvite(workCtx, email)
	if err != nil {
		if errors.Is(err, errAccountSession) {
			f.log.Warn("bdsmlr invite needs a session", "err", err)
			return bot.EditEphemeral(s, i, "The bdsmlr session is missing or expired. An admin needs to set `BDSMLR_SESSION` to a fresh cookie header copied from a signed-in account.bdsmlr.com session.")
		}
		f.log.Warn("bdsmlr invite failed", "email", email, "err", err)
		return bot.EditEphemeral(s, i, "I could not send the invite: "+err.Error())
	}

	f.log.Info("bdsmlr invite sent", "email", email, "message", message)
	return bot.EditEphemeral(s, i, fmt.Sprintf("Invitation sent to **%s**.", email))
}
