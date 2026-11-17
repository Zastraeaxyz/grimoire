package rss

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/store"
)

// pollAll checks every feed once. Errors are recorded per feed and shown by
// /rss list; they never interrupt the poller.
func (f *Feature) pollAll(s *discordgo.Session) {
	f.pollMu.Lock()
	defer f.pollMu.Unlock()

	ctx := f.ctx

	feeds, err := f.store.ListFeeds(ctx, f.guildID)
	if err != nil {
		f.log.Error("loading feeds for polling", "err", err)
		return
	}

	for idx := range feeds {
		if ctx.Err() != nil {
			return
		}
		feed := &feeds[idx]
		posted, err := f.pollFeed(ctx, s, feed)
		if err != nil {
			f.log.Warn("polling feed", "feed_id", feed.ID, "url", feed.URL, "err", err)
			continue
		}
		if posted > 0 {
			f.log.Info("posted feed items",
				"feed_id", feed.ID,
				"channel_id", feed.ChannelID,
				"count", posted,
			)
		}
	}
}

// pollFeed fetches one feed, posts unseen items oldest first, and records
// each one so a restart cannot repeat it.
func (f *Feature) pollFeed(parent context.Context, s *discordgo.Session, feed *store.Feed) (int, error) {
	ctx, cancel := context.WithTimeout(parent, feedTimeout)
	defer cancel()

	// Feeds created before the domain column existed need one unconditional
	// fetch so their site can be identified.
	etag, lastModified := feed.ETag, feed.LastModified
	if feed.Domain == "" {
		etag, lastModified = "", ""
	}

	result, err := f.fetchFeed(ctx, feed.URL, etag, lastModified)
	if err != nil {
		f.recordFetchError(feed, err)
		return 0, err
	}
	if result.NotModified {
		return 0, f.store.UpdateFeedFetch(ctx, feed.ID, "", "", "", "", "")
	}

	parsed, err := parseFeed(result.Body)
	if err != nil {
		f.recordFetchError(feed, fmt.Errorf("parsing feed: %w", err))
		return 0, err
	}
	domain := feedDomain(parsed, feed.URL)
	if err := f.store.UpdateFeedFetch(ctx, feed.ID, parsed.Title, domain, result.ETag, result.LastModified, ""); err != nil {
		f.log.Warn("saving feed state", "feed_id", feed.ID, "err", err)
	}
	if parsed.Title != "" {
		feed.Title = parsed.Title
	}
	if domain != "" {
		feed.Domain = domain
	}

	seen, err := f.store.SeenItemIDs(ctx, feed.ID)
	if err != nil {
		return 0, fmt.Errorf("loading seen items: %w", err)
	}

	fresh := make([]FeedItem, 0, len(parsed.Items))
	for _, item := range parsed.Items {
		if _, ok := seen[item.GUID]; ok {
			continue
		}
		fresh = append(fresh, item)
	}
	if len(fresh) == 0 {
		f.prune(feed.ID)
		return 0, nil
	}
	if len(fresh) > maxItemsPerFetch {
		// Page one is capped anyway; keep the newest items and wait for the
		// next cycle for the rest.
		fresh = fresh[:maxItemsPerFetch]
	}

	// Feeds list newest first. Posting oldest first keeps the channel in
	// chronological order.
	reverse(fresh)

	posted := 0
	for _, item := range fresh {
		messageID, err := f.postItem(ctx, s, feed, item)
		if err != nil {
			f.recordFetchError(feed, fmt.Errorf("posting item: %w", err))
			return posted, err
		}
		f.markItem(feed.ID, item.GUID, messageID)
		posted++
	}

	f.prune(feed.ID)
	return posted, nil
}

// postItem sends one item with its downloaded media. If Discord rejects the
// upload, the item is retried as a text-only embed so the post is not lost.
func (f *Feature) postItem(ctx context.Context, s *discordgo.Session, feed *store.Feed, item FeedItem) (string, error) {
	msg := f.buildMessage(ctx, feed, item)
	if len(msg.Files) == 0 {
		sent, err := s.ChannelMessageSendComplex(feed.ChannelID, msg)
		if err != nil {
			return "", err
		}
		return sent.ID, nil
	}

	sent, err := s.ChannelMessageSendComplex(feed.ChannelID, msg)
	if err == nil {
		return sent.ID, nil
	}

	f.log.Warn("posting feed item with media failed; retrying without attachments",
		"feed_id", feed.ID,
		"guid", item.GUID,
		"err", err,
	)
	sent, fallbackErr := s.ChannelMessageSendComplex(feed.ChannelID, &discordgo.MessageSend{
		Embeds: msg.Embeds,
	})
	if fallbackErr != nil {
		return "", fmt.Errorf("%w (without media: %v)", err, fallbackErr)
	}
	return sent.ID, nil
}

func (f *Feature) recordFetchError(feed *store.Feed, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if updateErr := f.store.UpdateFeedFetch(ctx, feed.ID, "", "", "", "", err.Error()); updateErr != nil {
		f.log.Warn("recording feed error", "feed_id", feed.ID, "err", updateErr)
	}
}

// markItem records a posted item with its own short deadline, because the
// poll deadline may already have expired after a slow upload.
func (f *Feature) markItem(feedID int64, guid, messageID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.store.MarkFeedItem(ctx, feedID, guid, messageID); err != nil {
		f.log.Error("recording posted item",
			"feed_id", feedID,
			"guid", guid,
			"message_id", messageID,
			"err", err,
		)
	}
}

func (f *Feature) prune(feedID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.store.PruneFeedItems(ctx, feedID, pruneKeepItems); err != nil {
		f.log.Warn("pruning feed items", "feed_id", feedID, "err", err)
	}
}
