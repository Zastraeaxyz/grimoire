package chastisafe

import (
	"strings"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
)

const messageChunkLimit = 1900

// sendEphemeralChunks replaces a deferred reply and follows up with any extra
// chunks so large lists are not truncated.
func sendEphemeralChunks(s *discordgo.Session, i *discordgo.InteractionCreate, text string) error {
	chunks := splitText(text, messageChunkLimit)
	if len(chunks) == 0 {
		chunks = []string{"Nothing to show."}
	}
	if err := bot.EditEphemeral(s, i, chunks[0]); err != nil {
		return err
	}
	for _, chunk := range chunks[1:] {
		if _, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: chunk,
			Flags:   discordgo.MessageFlagsEphemeral,
		}); err != nil {
			return err
		}
	}
	return nil
}

// splitText splits a message into newline-friendly chunks no longer than
// limit runes.
func splitText(text string, limit int) []string {
	if limit < 1 {
		limit = messageChunkLimit
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	chunks := make([]string, 0, 2)
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		chunks = append(chunks, b.String())
		b.Reset()
	}

	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		for len(runes) > limit {
			flush()
			chunks = append(chunks, string(runes[:limit]))
			runes = runes[limit:]
		}
		line = string(runes)
		current := utf8.RuneCountInString(b.String())
		if current > 0 && current+1+len(runes) > limit {
			flush()
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	flush()
	return chunks
}
