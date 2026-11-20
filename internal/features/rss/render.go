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

// buildMessages renders one feed item, downloading its media so the post does
// not depend on expiring CDN links. A lone image goes inside the embed; a set
// keeps its images as attachments so Discord shows them as one gallery, with
// extra messages once the per-message attachment limit is reached.
func (f *Feature) buildMessages(ctx context.Context, feed *store.Feed, item FeedItem) []*discordgo.MessageSend {
	item = f.loadRedditGallery(ctx, item)

	embed := &discordgo.MessageEmbed{
		Color:       embedColor,
		Author:      embedAuthor(feed, item),
		Title:       truncateText(displayTitle(item), embedTitleLimit),
		Description: embedDescription(item),
		Footer:      &discordgo.MessageEmbedFooter{Text: footerText(feed, item)},
	}
	if item.Link != "" {
		embed.URL = item.Link
	}

	if !item.Published.IsZero() {
		embed.Timestamp = item.Published.UTC().Format(time.RFC3339)
	}

	media := f.downloadMedia(ctx, item)
	if len(media) == 1 && media[0].isImage() {
		embed.Image = &discordgo.MessageEmbedImage{URL: "attachment://" + media[0].FileName}
	}

	msg := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
	}

	if len(media) <= maxAttachments {
		msg.Files = mediaFiles(media)
		return []*discordgo.MessageSend{msg}
	}

	msg.Files = mediaFiles(media[:maxAttachments])
	messages := []*discordgo.MessageSend{msg}
	for start := maxAttachments; start < len(media); start += maxAttachments {
		end := start + maxAttachments
		if end > len(media) {
			end = len(media)
		}
		messages = append(messages, &discordgo.MessageSend{Files: mediaFiles(media[start:end])})
	}
	return messages
}

func mediaFiles(media []downloadedMedia) []*discordgo.File {
	files := make([]*discordgo.File, 0, len(media))
	for _, m := range media {
		files = append(files, &discordgo.File{
			Name:   m.FileName,
			Reader: bytes.NewReader(m.Data),
		})
	}
	return files
}

// clearAttachmentImages drops embed images that point at uploaded files, for
// the text-only retry when Discord rejects the upload.
func clearAttachmentImages(embeds []*discordgo.MessageEmbed) {
	for _, embed := range embeds {
		if embed.Image != nil && strings.HasPrefix(embed.Image.URL, "attachment://") {
			embed.Image = nil
		}
		if embed.Thumbnail != nil && strings.HasPrefix(embed.Thumbnail.URL, "attachment://") {
			embed.Thumbnail = nil
		}
	}
}

// embedDescription renders the post body with every category as a hashtag.
// The body gives up room first so the tags are never cut for space.
func embedDescription(item FeedItem) string {
	text := htmlToText(item.Content)
	if text == "" {
		text = htmlToText(item.Summary)
	}

	tags := categoryTags(item.Categories)
	if tags == "" {
		return truncateText(text, embedDescriptionLimit)
	}
	if text == "" {
		return truncateText(tags, embedDescriptionLimit)
	}

	room := embedDescriptionLimit - len([]rune(tags)) - 2 - 3 // blank line and ellipsis
	if room <= 0 {
		return truncateText(tags, embedDescriptionLimit)
	}
	return truncateText(text, room) + "\n\n" + tags
}

// categoryTags formats categories as "#name" tokens. Categories are already
// normalized and deduplicated by the parser.
func categoryTags(categories []string) string {
	tags := make([]string, 0, len(categories))
	for _, category := range categories {
		name := strings.TrimSpace(strings.TrimLeft(category, "#"))
		if name == "" {
			continue
		}
		tags = append(tags, "#"+name)
	}
	return strings.Join(tags, " ")
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

// embedAuthor names the Reddit poster, linked to their profile. Other feeds
// name the feed itself, linked to the post.
func embedAuthor(feed *store.Feed, item FeedItem) *discordgo.MessageEmbedAuthor {
	if item.Reddit && item.Author != "" {
		return &discordgo.MessageEmbedAuthor{
			Name: "u/" + item.Author,
			URL:  redditProfileURL(item.Author),
		}
	}
	return &discordgo.MessageEmbedAuthor{Name: feedDisplayName(feed), URL: item.Link}
}

// footerText identifies a post's origin. Reddit posts name their feed and call
// out galleries, because the first post only carries the cover.
func footerText(feed *store.Feed, item FeedItem) string {
	site := siteName(item.Link)
	if !item.Reddit {
		return site
	}

	parts := make([]string, 0, 3)
	if item.Gallery {
		parts = append(parts, "Gallery")
	}
	if name := feedDisplayName(feed); name != "" {
		parts = append(parts, name)
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
