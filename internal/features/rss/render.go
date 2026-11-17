package rss

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/store"
)

const (
	embedColor            = 0xEC4899
	embedDescriptionLimit = 3800
	embedTitleLimit       = 240
	embedAuthorLimit      = 200
)

// buildMessage renders one feed item, downloading its media so the post does
// not depend on expiring CDN links.
func (f *Feature) buildMessage(ctx context.Context, feed *store.Feed, item FeedItem) *discordgo.MessageSend {
	text := htmlToText(item.Content)
	if text == "" {
		text = htmlToText(item.Summary)
	}

	embed := &discordgo.MessageEmbed{
		Color:       embedColor,
		Author:      &discordgo.MessageEmbedAuthor{Name: feedDisplayName(feed), URL: item.Link},
		Title:       truncateText(displayTitle(item), embedTitleLimit),
		Description: truncateText(text, embedDescriptionLimit),
		Footer:      &discordgo.MessageEmbedFooter{Text: footerText(item)},
	}
	if item.Link != "" {
		embed.URL = item.Link
	}

	if !item.Published.IsZero() {
		embed.Timestamp = item.Published.UTC().Format(time.RFC3339)
	}

	msg := &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: redditLoadButton(item),
	}

	for _, media := range f.downloadMedia(ctx, item) {
		msg.Files = append(msg.Files, &discordgo.File{
			Name:   media.FileName,
			Reader: bytes.NewReader(media.Data),
		})
	}
	return msg
}

// displayTitle avoids repeating machine-generated titles that carry no
// information, such as "Post 877982930".
func displayTitle(item FeedItem) string {
	if title := cleanTitle(item.Title); title != "" {
		return title
	}
	return "New post"
}

func feedDisplayName(feed *store.Feed) string {
	name := cleanFeedTitle(feed.Title)
	if name == "" {
		name = siteName(feed.URL)
	}
	if name == "" {
		name = "RSS"
	}
	return truncateText(name, embedAuthorLimit)
}

// domainSuffix returns " on example.com" for lists and replies.
func domainSuffix(feed *store.Feed) string {
	if feed.Domain == "" {
		return ""
	}
	return " on " + feed.Domain
}

// feedLabel identifies a feed as username plus site.
func feedLabel(feed *store.Feed) string {
	return feedDisplayName(feed) + domainSuffix(feed)
}

// cleanFeedTitle shortens profile-style feed titles such as
// "Imaglr - zastraea's Profile" to the person's name.
func cleanFeedTitle(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	if strings.HasSuffix(name, "Profile") {
		if idx := strings.LastIndex(name, " - "); idx > 0 {
			tail := strings.TrimSpace(name[idx+3:])
			tail = strings.TrimSuffix(tail, "'s Profile")
			tail = strings.TrimSuffix(tail, "\u2019s Profile")
			if tail != "" {
				return tail
			}
		}
	}
	return name
}

func siteName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
}

// footerText identifies a post's origin. Reddit posts also name the author,
// and galleries are called out because the first post only carries the cover.
func footerText(item FeedItem) string {
	site := siteName(item.Link)
	if !item.Reddit {
		return site
	}

	parts := make([]string, 0, 3)
	if item.Gallery {
		parts = append(parts, "Gallery")
	}
	if item.Author != "" {
		parts = append(parts, "u/"+item.Author)
	}
	if site != "" {
		parts = append(parts, site)
	}
	return strings.Join(parts, " \u2022 ")
}

// messageLink builds a jump link for a posted item.
func messageLink(guildID, channelID, messageID string) string {
	if guildID == "" || channelID == "" || messageID == "" {
		return ""
	}
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}
