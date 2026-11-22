package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	"grimoire/internal/store"
)

const (
	defaultSiteURL  = "https://bdsmlr.zastraea.xyz"
	defaultTimezone = "UTC"

	// publishBudget bounds one dashboard refresh, including every Umami call.
	publishBudget = 2 * time.Minute
)

var (
	errNoChannel         = errors.New("no stats channel is configured")
	notConfiguredMessage = "Umami is not configured, so there is nothing to report. Set `UMAMI_URL`, `UMAMI_WEBSITE_ID`, and `UMAMI_API_KEY`."
)

// Options configures the stats feature.
type Options struct {
	GuildID string
	Store   *store.Store
	Log     *slog.Logger

	// SiteURL is the bbdsmlr origin used for blog links.
	SiteURL  string
	Timezone string
	Times    []string

	// Umami access. The feature is disabled unless all three are set.
	UmamiURL       string
	UmamiWebsiteID string
	UmamiAPIKey    string
}

// Feature posts the bbdsmlr analytics dashboard on a schedule and on demand.
type Feature struct {
	ctx     context.Context
	guildID string
	store   *store.Store
	log     *slog.Logger
	siteURL string
	loc     *time.Location
	times   []clockTime
	umami   *umamiClient

	startOnce sync.Once

	// publishMu keeps a scheduled report and /stats post from editing the
	// same dashboard at once.
	publishMu sync.Mutex

	statusMu  sync.Mutex
	nextRunAt time.Time
	lastRun   time.Time
	lastError string
}

// New builds the stats feature. The context controls the scheduler lifetime.
func New(ctx context.Context, opts Options) *Feature {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}

	siteURL := strings.TrimRight(strings.TrimSpace(opts.SiteURL), "/")
	if siteURL == "" {
		siteURL = defaultSiteURL
	}

	zone := strings.TrimSpace(opts.Timezone)
	if zone == "" {
		zone = defaultTimezone
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		opts.Log.Warn("stats timezone is invalid; using UTC", "timezone", zone, "err", err)
		loc = time.UTC
	}

	times, err := parseClockTimes(opts.Times)
	if err != nil {
		opts.Log.Warn("stats schedule is invalid; only /stats post will work", "err", err)
		times = nil
	}

	f := &Feature{
		ctx:     ctx,
		guildID: opts.GuildID,
		store:   opts.Store,
		log:     opts.Log,
		siteURL: siteURL,
		loc:     loc,
		times:   times,
	}

	url := strings.TrimSpace(opts.UmamiURL)
	websiteID := strings.TrimSpace(opts.UmamiWebsiteID)
	apiKey := strings.TrimSpace(opts.UmamiAPIKey)
	if url == "" || websiteID == "" || apiKey == "" {
		opts.Log.Warn("stats reports are disabled until UMAMI_URL, UMAMI_WEBSITE_ID and UMAMI_API_KEY are set")
	} else {
		f.umami = newUmamiClient(url, websiteID, apiKey)
	}
	return f
}

// Register wires the feature into the bot. The command is admin-only, matching
// /settings, and the ready hook starts the scheduler once.
func (f *Feature) Register(b *bot.Bot) {
	b.AddCommand(command(), f.handleCommand)
	b.AddReadyHook(f.handleReady)
}

func (f *Feature) enabled() bool {
	return f.umami != nil
}

func (f *Feature) handleReady(ctx context.Context, s *discordgo.Session) error {
	f.startOnce.Do(func() {
		go f.run(s)
	})
	return nil
}

// run waits for the configured wall-clock times and refreshes the dashboard.
// A missing channel is not an error worth a stack trace; the next run retries.
func (f *Feature) run(s *discordgo.Session) {
	if !f.enabled() || len(f.times) == 0 {
		return
	}

	// Let command registration and the gateway settle first.
	select {
	case <-f.ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	for {
		next := nextRun(time.Now(), f.times, f.loc)
		f.setNextRun(next)
		f.log.Info("next stats report", "at", next.UTC().Format(time.RFC3339))

		timer := time.NewTimer(time.Until(next))
		select {
		case <-f.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			ctx, cancel := context.WithTimeout(f.ctx, publishBudget)
			_, _, err := f.publish(ctx, s)
			cancel()
			if err != nil {
				if errors.Is(err, errNoChannel) {
					f.log.Debug("scheduled stats report skipped; no channel configured")
					continue
				}
				f.log.Warn("posting the scheduled stats report failed", "err", err)
			}
		}
	}
}

// publish collects a report and creates or edits the dashboard message.
func (f *Feature) publish(ctx context.Context, s *discordgo.Session) (channelID, messageID string, err error) {
	f.publishMu.Lock()
	defer f.publishMu.Unlock()

	settings, err := f.store.GetGuildSettings(ctx, f.guildID)
	if err != nil {
		err = fmt.Errorf("loading stats settings: %w", err)
		f.setError(err)
		return "", "", err
	}
	if settings == nil || settings.StatsChannelID == "" {
		f.setError(errNoChannel)
		return "", "", errNoChannel
	}
	channelID = settings.StatsChannelID

	rep, err := f.collect(ctx, time.Now())
	if err != nil {
		f.setError(err)
		return "", "", err
	}
	embeds := buildEmbeds(rep, f.siteURL)

	if settings.StatsMessageID != "" {
		_, editErr := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel: channelID,
			ID:      settings.StatsMessageID,
			Embeds:  &embeds,
		})
		if editErr == nil {
			f.markRun(time.Now())
			return channelID, settings.StatsMessageID, nil
		}
		// A deleted dashboard, or a message from a previous channel, cannot be
		// edited. Fall through and post a fresh one.
		f.log.Warn("editing the stats dashboard failed; posting a new one",
			"channel_id", channelID,
			"message_id", settings.StatsMessageID,
			"err", editErr,
		)
	}

	msg, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Embeds: embeds})
	if err != nil {
		err = fmt.Errorf("posting the stats dashboard: %w", err)
		f.setError(err)
		return "", "", err
	}
	if err := f.store.SetStatsMessage(ctx, f.guildID, channelID, msg.ID); err != nil {
		f.log.Warn("saving the stats dashboard message", "err", err)
	}
	// Pinning is a convenience; a channel without Manage Messages still gets
	// its dashboard.
	if err := s.ChannelMessagePin(channelID, msg.ID); err != nil {
		f.log.Debug("pinning the stats dashboard", "channel_id", channelID, "err", err)
	}

	f.markRun(time.Now())
	f.log.Info("posted the stats dashboard", "channel_id", channelID, "message_id", msg.ID)
	return channelID, msg.ID, nil
}

func (f *Feature) setNextRun(at time.Time) {
	f.statusMu.Lock()
	f.nextRunAt = at
	f.statusMu.Unlock()
}

func (f *Feature) markRun(at time.Time) {
	f.statusMu.Lock()
	f.lastRun = at
	f.lastError = ""
	f.statusMu.Unlock()
}

func (f *Feature) setError(err error) {
	if err == nil {
		return
	}
	f.statusMu.Lock()
	f.lastError = err.Error()
	f.statusMu.Unlock()
}
