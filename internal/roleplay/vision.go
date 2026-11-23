package roleplay

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/ai"
)

// visionImages collects image attachments from the current message and the
// message it replies to. Only Discord-hosted images of a supported type and
// size are used, so the model never fetches an arbitrary link.
func (f *Feature) visionImages(current *discordgo.MessageCreate, referenced *discordgo.Message) []ai.Image {
	if f == nil || !f.visionEnabled {
		return nil
	}

	maxImages := f.visionMaxImages
	if maxImages < 1 {
		maxImages = 1
	}
	images := 0

	result := make([]ai.Image, 0, maxImages)
	add := func(attachments []*discordgo.MessageAttachment, label string) {
		for _, attachment := range attachments {
			if images >= maxImages {
				return
			}
			if attachment == nil {
				continue
			}
			if !supportedImageType(attachment.ContentType) {
				f.log.Debug("skipping unsupported image attachment",
					"filename", attachment.Filename,
					"type", attachment.ContentType,
				)
				continue
			}
			if attachment.Size <= 0 || int64(attachment.Size) > f.visionMaxBytes {
				f.log.Debug("skipping oversized image attachment",
					"filename", attachment.Filename,
					"size", attachment.Size,
				)
				continue
			}

			imageURL := discordImageURL(attachment)
			if imageURL == "" {
				f.log.Debug("skipping image attachment without a Discord URL",
					"filename", attachment.Filename,
				)
				continue
			}

			result = append(result, ai.Image{
				Label:  fmt.Sprintf("%s: %s", label, attachment.Filename),
				URL:    imageURL,
				Detail: f.visionDetail,
			})
			images++
		}
	}

	if current != nil {
		add(current.Attachments, "Image on the current message")
	}
	if referenced != nil {
		add(referenced.Attachments, "Image on the referenced message")
	}

	return result
}

func supportedImageType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

// discordImageURL returns the first attachment URL hosted by Discord. Expiring
// CDN links and proxied links both work; foreign hosts do not.
func discordImageURL(attachment *discordgo.MessageAttachment) string {
	for _, candidate := range []string{attachment.URL, attachment.ProxyURL} {
		if candidate == "" {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Scheme != "https" {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if strings.HasSuffix(host, ".discordapp.com") || strings.HasSuffix(host, ".discordapp.net") {
			return candidate
		}
	}
	return ""
}
