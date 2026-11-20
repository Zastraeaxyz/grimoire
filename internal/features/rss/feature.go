package rss

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	"grimoire/internal/store"
)

const (
	feedTimeout      = 3 * time.Minute
	interactionWork  = 5 * time.Minute
	defaultBackfill  = 3
	maxItemsPerFetch = 20
	pruneKeepItems   = 500
)

// Options configures the RSS feature.
type Options struct {
	GuildID            string
	Store              *store.Store
	Log                *slog.Logger
	PollInterval       time.Duration
	MaxAttachmentBytes int64

	// Reddit listing pages need a signed-in session. The agent and cookie are
	// only used for reddit feeds.
	RedditSession     string
	RedditUserAgent   string
	RedditIncludeNSFW bool
}

// Feature polls feeds and posts new items into their channels.
type Feature struct {
	ctx                context.Context
	guildID            string
	store              *store.Store
	log                *slog.Logger
	pollInterval       time.Duration
	maxAttachmentBytes int64
	httpClient         *http.Client

	redditSession     string
	redditUserAgent   string
	redditIncludeNSFW bool
	redditMu          sync.Mutex
	redditLastRequest time.Time
	redditLoading     sync.Map

	startOnce sync.Once
	pollMu    sync.Mutex
}

// New builds the RSS feature. The context controls the poller lifetime.
func New(ctx context.Context, opts Options) *Feature {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Hour
	}
	if opts.MaxAttachmentBytes <= 0 {
		opts.MaxAttachmentBytes = 10 << 20
	}
	if opts.RedditUserAgent == "" {
		opts.RedditUserAgent = defaultRedditUserAgent
	}

	return &Feature{
		ctx:                ctx,
		guildID:            opts.GuildID,
		store:              opts.Store,
		log:                opts.Log,
		pollInterval:       opts.PollInterval,
		maxAttachmentBytes: opts.MaxAttachmentBytes,
		httpClient:         &http.Client{Timeout: 60 * time.Second},
		redditSession:      normalizeRedditCookie(opts.RedditSession),
		redditUserAgent:    opts.RedditUserAgent,
		redditIncludeNSFW:  opts.RedditIncludeNSFW,
	}
}

// Register wires the feature into the bot.
func (f *Feature) Register(b *bot.Bot) {
	b.AddCommand(command(), f.handleCommand)
	b.AddAutocomplete(CommandName, f.handleAutocomplete)
	b.AddComponent("reddit", f.handleRedditComponent)
	b.AddReadyHook(f.handleReady)
}

// handleReady starts the poller once. Reconnects do not start more loops.
func (f *Feature) handleReady(ctx context.Context, s *discordgo.Session) error {
	f.startOnce.Do(func() {
		go f.run(s)
	})
	return nil
}

func (f *Feature) run(s *discordgo.Session) {
	// Let command registration settle before the first outbound post.
	select {
	case <-f.ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}

	f.warnMissingRedditSession()
	f.pollAll(s)

	ticker := time.NewTicker(f.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			return
		case <-ticker.C:
			f.pollAll(s)
		}
	}
}

func (f *Feature) handleCommand(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	if i.Member == nil || i.Member.Permissions&discordgo.PermissionManageGuild == 0 {
		return bot.ReplyEphemeral(s, i, "You need the **Manage Server** permission to configure feeds.")
	}

	data := i.ApplicationCommandData()
	if len(data.Options) == 0 {
		return bot.ReplyEphemeral(s, i, "Use `/rss add`, `/rss list`, or `/rss remove`.")
	}

	if err := bot.DeferEphemeral(s, i); err != nil {
		return err
	}

	// These subcommands download media and may post several messages, which
	// takes longer than the interaction budget the bot handler sets.
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interactionWork)
	defer cancel()

	sub := data.Options[0]
	switch sub.Name {
	case "add":
		return f.add(workCtx, s, i, sub.Options)
	case "remove":
		return f.remove(workCtx, s, i, sub.Options)
	case "list":
		return f.list(workCtx, s, i)
	case "test":
		return f.test(workCtx, s, i, sub.Options)
	case "check":
		return f.check(workCtx, s, i)
	default:
		return bot.EditEphemeral(s, i, "Unknown subcommand.")
	}
}

func (f *Feature) add(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	rawURL := ""
	channelID := i.ChannelID
	backfill := defaultBackfill

	for _, opt := range opts {
		switch opt.Name {
		case "url":
			rawURL = opt.StringValue()
		case "channel":
			if id, ok := opt.Value.(string); ok && id != "" {
				channelID = id
			}
		case "backfill":
			backfill = int(opt.IntValue())
		}
	}

	feedURL, err := normalizeFeedURL(rawURL)
	if err != nil {
		return bot.EditEphemeral(s, i, err.Error())
	}

	channel, err := f.validateChannel(s, channelID)
	if err != nil {
		return bot.EditEphemeral(s, i, err.Error())
	}

	existing, err := f.store.GetFeedByURL(ctx, f.guildID, feedURL)
	if err != nil {
		return fmt.Errorf("looking up feed: %w", err)
	}
	if existing != nil {
		return bot.EditEphemeral(s, i, fmt.Sprintf(
			"That feed is already followed in <#%s>. Remove it first with `/rss remove`.",
			existing.ChannelID,
		))
	}

	result, err := f.fetchFeed(ctx, feedURL, "", "")
	if err != nil {
		if errors.Is(err, errNotAFeed) {
			return bot.EditEphemeral(s, i, "That URL does not return a readable RSS or Atom feed.")
		}
		return bot.EditEphemeral(s, i, "I could not load that feed: "+err.Error())
	}
	parsed := result.Feed
	if len(parsed.Items) == 0 {
		return bot.EditEphemeral(s, i, "That feed is readable but has no items to follow yet.")
	}

	feed := &store.Feed{
		GuildID:   f.guildID,
		URL:       feedURL,
		ChannelID: channelID,
		Title:     parsed.Title,
		Domain:    feedDomain(parsed, feedURL),
		AddedBy:   addedByUserID(i),
	}
	id, err := f.store.CreateFeed(ctx, feed)
	if err != nil {
		return fmt.Errorf("saving feed: %w", err)
	}
	feed.ID = id

	if err := f.store.UpdateFeedFetch(ctx, id, parsed.Title, feed.Domain, result.ETag, result.LastModified, ""); err != nil {
		f.log.Warn("saving feed fetch state", "feed_id", id, "err", err)
	}

	posted, firstMessageID, failures := f.seed(ctx, s, feed, parsed.Items, backfill)

	// Everything on the first page is now handled, so past items never
	// appear later.
	for _, item := range parsed.Items {
		if err := f.store.MarkFeedItem(ctx, id, item.GUID, ""); err != nil {
			f.log.Warn("marking seeded feed item", "feed_id", id, "guid", item.GUID, "err", err)
		}
	}

	f.log.Info("feed added",
		"feed_id", id,
		"url", feedURL,
		"channel_id", channelID,
		"domain", feed.Domain,
		"added_by", feed.AddedBy,
		"items", len(parsed.Items),
		"backfilled", posted,
	)

	var reply strings.Builder
	fmt.Fprintf(&reply, "Following **%s**", feedDisplayName(feed))
	if feed.Domain != "" {
		fmt.Fprintf(&reply, " on %s", feed.Domain)
	}
	fmt.Fprintf(&reply, " in <#%s>.\n", channelID)
	if posted > 0 {
		fmt.Fprintf(&reply, "Posted %d recent item", posted)
		if posted != 1 {
			reply.WriteString("s")
		}
		if link := messageLink(f.guildID, channelID, firstMessageID); link != "" {
			reply.WriteString(": " + link)
		}
		reply.WriteString("\n")
	} else if backfill > 0 {
		reply.WriteString("I could not post the recent items, so the next new post will be the first one.\n")
	}
	if len(failures) > 0 {
		fmt.Fprintf(&reply, "%d item(s) failed to post; I will not retry them now.\n", len(failures))
	}
	fmt.Fprintf(&reply, "I check for new posts every %s.", humanDuration(f.pollInterval))
	if !channel.NSFW {
		reply.WriteString("\n\nHeads up: this channel is not age-restricted, and these sources are NSFW.")
	}
	return bot.EditEphemeral(s, i, reply.String())
}

// seed posts the newest items from the first page, oldest first.
func (f *Feature) seed(ctx context.Context, s *discordgo.Session, feed *store.Feed, items []FeedItem, backfill int) (int, string, []error) {
	if backfill <= 0 {
		return 0, "", nil
	}
	if backfill > len(items) {
		backfill = len(items)
	}

	selected := append([]FeedItem(nil), items[:backfill]...)
	reverse(selected)

	posted := 0
	firstMessageID := ""
	var failures []error

	for _, item := range selected {
		messageID, err := f.postItem(ctx, s, feed, item)
		if err != nil {
			f.log.Warn("seeding feed item", "feed_id", feed.ID, "guid", item.GUID, "err", err)
			failures = append(failures, err)
			continue
		}
		if firstMessageID == "" {
			firstMessageID = messageID
		}
		posted++
		if err := f.store.MarkFeedItem(ctx, feed.ID, item.GUID, messageID); err != nil {
			f.log.Warn("recording seeded item", "feed_id", feed.ID, "guid", item.GUID, "err", err)
		}
	}
	return posted, firstMessageID, failures
}

func (f *Feature) remove(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	var feedID int64
	for _, opt := range opts {
		if opt.Name == "feed" {
			feedID = opt.IntValue()
		}
	}
	if feedID == 0 {
		return bot.EditEphemeral(s, i, "Pick a feed to remove.")
	}

	feeds, err := f.store.ListFeeds(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading feeds: %w", err)
	}
	var target *store.Feed
	for idx := range feeds {
		if feeds[idx].ID == feedID {
			target = &feeds[idx]
			break
		}
	}
	if target == nil {
		return bot.EditEphemeral(s, i, "That feed is not in the list anymore. Run `/rss list`.")
	}

	if err := f.store.DeleteFeed(ctx, f.guildID, feedID); err != nil {
		return fmt.Errorf("deleting feed: %w", err)
	}
	f.log.Info("feed removed", "feed_id", feedID, "url", target.URL)
	return bot.EditEphemeral(s, i, fmt.Sprintf(
		"Stopped following **%s**%s. Messages already posted stay in <#%s>.",
		feedDisplayName(target), domainSuffix(target), target.ChannelID,
	))
}

func (f *Feature) list(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	feeds, err := f.store.ListFeeds(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading feeds: %w", err)
	}
	if len(feeds) == 0 {
		return bot.EditEphemeral(s, i, "No feeds yet. Add one with `/rss add url:<feed URL> optional-channel:#channel`.")
	}

	var out strings.Builder
	fmt.Fprintf(&out, "**Feeds (%d)**\n", len(feeds))
	for _, feed := range feeds {
		fmt.Fprintf(&out, "\n`#%d` **%s**%s → <#%s>\n%s\n",
			feed.ID, feedDisplayName(&feed), domainSuffix(&feed), feed.ChannelID, feed.URL)
		fmt.Fprintf(&out, "Last checked %s.", humanAgo(feed.LastCheckedAt))
		if feed.LastError != "" {
			fmt.Fprintf(&out, " Last error: %s", feed.LastError)
		}
		out.WriteString("\n")
	}
	return bot.EditEphemeral(s, i, truncateText(out.String(), 1900))
}

func (f *Feature) test(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) error {
	rawURL := ""
	for _, opt := range opts {
		if opt.Name == "url" {
			rawURL = opt.StringValue()
		}
	}
	feedURL, err := normalizeFeedURL(rawURL)
	if err != nil {
		return bot.EditEphemeral(s, i, err.Error())
	}

	result, err := f.fetchFeed(ctx, feedURL, "", "")
	if err != nil {
		if errors.Is(err, errNotAFeed) {
			return bot.EditEphemeral(s, i, "That URL does not return a readable RSS or Atom feed.")
		}
		return bot.EditEphemeral(s, i, "I could not load that feed: "+err.Error())
	}
	parsed := result.Feed
	if len(parsed.Items) == 0 {
		return bot.EditEphemeral(s, i, "That feed has no items right now.")
	}

	feed := &store.Feed{GuildID: f.guildID, URL: feedURL, ChannelID: i.ChannelID, Title: parsed.Title}
	messages := f.buildMessages(ctx, feed, parsed.Items[0])

	_, err = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: "Preview of the newest item:",
		Embeds:  messages[0].Embeds,
		Files:   messages[0].Files,
	})
	if err != nil {
		return bot.EditEphemeral(s, i, "I could not render that item: "+err.Error())
	}
	for _, extra := range messages[1:] {
		if _, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Files: extra.Files,
		}); err != nil {
			f.log.Warn("posting extra preview media failed", "err", err)
		}
	}
	return nil
}

func (f *Feature) check(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	feeds, err := f.store.ListFeeds(ctx, f.guildID)
	if err != nil {
		return fmt.Errorf("loading feeds: %w", err)
	}
	if len(feeds) == 0 {
		return bot.EditEphemeral(s, i, "No feeds to check yet. Add one with `/rss add`.")
	}

	posted := 0
	failures := 0
	f.pollMu.Lock()
	defer f.pollMu.Unlock()
	for idx := range feeds {
		count, err := f.pollFeed(ctx, s, &feeds[idx])
		posted += count
		if err != nil {
			failures++
		}
	}

	reply := fmt.Sprintf("Checked %d feed", len(feeds))
	if len(feeds) != 1 {
		reply += "s"
	}
	reply += fmt.Sprintf(" and posted %d new item", posted)
	if posted != 1 {
		reply += "s"
	}
	reply += "."
	if failures > 0 {
		reply += fmt.Sprintf(" %d feed(s) reported an error, see `/rss list`.", failures)
	}
	return bot.EditEphemeral(s, i, reply)
}

func (f *Feature) handleAutocomplete(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	feeds, err := f.store.ListFeeds(ctx, f.guildID)
	if err != nil {
		return err
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(feeds))
	for _, feed := range feeds {
		channelName := feed.ChannelID
		if ch, err := s.State.Channel(feed.ChannelID); err == nil && ch.Name != "" {
			channelName = ch.Name
		}
		label := fmt.Sprintf("#%d %s -> #%s", feed.ID, feedLabel(&feed), channelName)
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateText(label, 96),
			Value: feed.ID,
		})
		if len(choices) == 25 {
			break
		}
	}
	return bot.RespondAutocomplete(s, i, choices)
}

func (f *Feature) validateChannel(s *discordgo.Session, channelID string) (*discordgo.Channel, error) {
	ch, err := s.Channel(channelID)
	if err != nil {
		return nil, fmt.Errorf("I cannot see <#%s>. Check my channel permissions.", channelID)
	}
	if ch.Type != discordgo.ChannelTypeGuildText && ch.Type != discordgo.ChannelTypeGuildNews {
		return nil, errors.New("Pick a text channel for feed posts.")
	}

	perms, err := s.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return nil, fmt.Errorf("checking my permissions in <#%s>: %w", channelID, err)
	}
	required := int64(discordgo.PermissionViewChannel |
		discordgo.PermissionSendMessages |
		discordgo.PermissionEmbedLinks |
		discordgo.PermissionAttachFiles)
	if perms&required != required {
		return nil, fmt.Errorf("I need View Channel, Send Messages, Embed Links and Attach Files in <#%s>.", channelID)
	}
	return ch, nil
}

// normalizeFeedURL validates a URL and adds a scheme when the admin left it
// out. Fragments are dropped; query strings are kept because several feed
// URLs use them.
func normalizeFeedURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("Give me an RSS or Atom URL, for example `https://example.com/feed.xml`.")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("That is not a valid http(s) URL.")
	}
	if isRedditHost(u.Hostname()) {
		target, ok := redditTargetFromURL(u)
		if !ok {
			return "", errors.New("That Reddit URL is not a subreddit, user, or search feed. Use https://www.reddit.com/r/cats, https://www.reddit.com/user/name, or https://www.reddit.com/search?q=cats.")
		}
		return target.FeedURL, nil
	}
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

// addedByUserID records who configured a feed, for future auditing.
func addedByUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	return ""
}

func humanAgo(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes <= 1 {
			return "minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}
	hours := int(d.Hours())
	if d%time.Hour == 0 {
		if hours == 1 {
			return "hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, int(d.Minutes())-hours*60)
}

// reverse flips a slice in place, turning newest-first feed order into
// oldest-first posting order.
func reverse(items []FeedItem) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}
