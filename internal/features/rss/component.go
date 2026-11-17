package rss

import (
	"context"
	"net/url"
	"strings"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	"grimoire/internal/store"
)

const redditLoadPrefix = "reddit:load:"

// handleRedditComponent serves the "Load full post" button. It is open to
// every member: the button only reveals media that is already public on the
// linked post, and the post it edits belongs to the bot.
func (f *Feature) handleRedditComponent(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate) error {
	postID, ok := redditPostIDFromCustomID(i.MessageComponentData().CustomID)
	if !ok {
		return bot.ReplyEphemeral(s, i, "That button is no longer valid.")
	}

	if _, busy := f.redditLoading.LoadOrStore(postID, struct{}{}); busy {
		return bot.ReplyEphemeral(s, i, "That post is already loading.")
	}
	defer f.redditLoading.Delete(postID)

	if err := bot.DeferPublic(s, i); err != nil {
		return err
	}

	// The interaction budget is short; downloading a gallery can take much
	// longer. Detach from the handler deadline and give the work its own.
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interactionWork)
	defer cancel()

	item, err := f.fetchRedditPost(workCtx, postID)
	if err != nil {
		f.log.Warn("loading full reddit post", "post_id", postID, "err", err)
		return editInteractionResponse(s, i, "I could not load that post: "+err.Error(), nil, nil)
	}

	feed := &store.Feed{
		Title: redditFeedTitle(item.Link),
		URL:   item.Link,
	}
	msg := f.buildMessage(workCtx, feed, item)

	if err := editInteractionResponse(s, i, "", msg.Embeds, msg.Files); err != nil {
		return err
	}
	// Keep the original when it held the only media: if every download
	// failed, deleting it would replace a cover with a text-only post. The
	// button stays available so the load can be retried.
	if len(msg.Files) > 0 || len(item.Media) == 0 {
		f.deleteOriginalMessage(s, i)
	}
	return nil
}

// editInteractionResponse fills in a deferred response.
func editInteractionResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string, embeds []*discordgo.MessageEmbed, files []*discordgo.File) error {
	edit := &discordgo.WebhookEdit{}
	if content != "" {
		edit.Content = &content
	}
	if len(embeds) > 0 {
		edit.Embeds = &embeds
	}
	if len(files) > 0 {
		edit.Files = files
	}
	_, err := s.InteractionResponseEdit(i.Interaction, edit)
	return err
}

// deleteOriginalMessage removes the placeholder post once the full post is
// live, so the channel keeps one message instead of a cover and a duplicate.
// The bot deletes its own message, so no extra permission is needed.
func (f *Feature) deleteOriginalMessage(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Message == nil || i.ChannelID == "" || i.Message.ID == "" {
		return
	}
	if err := s.ChannelMessageDelete(i.ChannelID, i.Message.ID); err != nil {
		f.log.Warn("deleting original reddit post after loading the full post",
			"message_id", i.Message.ID,
			"err", err,
		)
	}
}

func redditPostIDFromCustomID(customID string) (string, bool) {
	raw := strings.TrimPrefix(customID, redditLoadPrefix)
	if raw == customID {
		return "", false
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || len(raw) > 20 {
		return "", false
	}
	for _, r := range raw {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", false
		}
	}
	return raw, true
}

// redditFeedTitle recovers "r/sub" or "u/name" from a post permalink, for the
// enriched message's author line.
func redditFeedTitle(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return "reddit"
	}
	parts := pathSegments(u.Path)
	if len(parts) < 2 {
		return "reddit"
	}
	switch strings.ToLower(parts[0]) {
	case "r":
		return "r/" + parts[1]
	case "user", "u":
		return "u/" + parts[1]
	}
	return "reddit"
}
