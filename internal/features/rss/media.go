package rss

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	// maxAttachments matches Discord's per-message attachment count.
	maxAttachments = 10
	// maxTotalAttachmentBytes keeps one message from ballooning. The per-file
	// cap is configurable; this is the ceiling across the whole post.
	maxTotalAttachmentBytes = 25 << 20
)

// downloadedMedia is an attachment ready to hand to Discord.
type downloadedMedia struct {
	FileName string
	Data     []byte
}

// downloadMedia fetches the images and videos for an item. Failures are
// logged and skipped: one dead CDN link must not stop the post.
func (f *Feature) downloadMedia(ctx context.Context, item FeedItem) []downloadedMedia {
	var (
		files []downloadedMedia
		total int64
	)

	for i, media := range item.Media {
		if len(files) >= maxAttachments {
			break
		}

		data, contentType, err := f.downloadOne(ctx, media.URL, f.maxAttachmentBytes)
		if err != nil {
			f.log.Debug("skipping unavailable feed media",
				"url", media.URL,
				"err", err,
			)
			continue
		}
		if total+int64(len(data)) > maxTotalAttachmentBytes {
			f.log.Warn("feed post exceeds attachment budget; dropping remaining media",
				"guid", item.GUID,
				"dropped_from", media.URL,
			)
			break
		}

		total += int64(len(data))
		files = append(files, downloadedMedia{
			FileName: fmt.Sprintf("media-%d%s", i+1, extensionFor(contentType, media.URL)),
			Data:     data,
		})
	}
	return files
}

func (f *Feature) downloadOne(ctx context.Context, mediaURL string, limit int64) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/*,video/*;q=0.9,*/*;q=0.5")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("media returned HTTP %d", resp.StatusCode)
	}

	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(contentType, "image/"), strings.HasPrefix(contentType, "video/"):
	case contentType == "application/octet-stream" && mediaExtension(mediaURL) != "":
	default:
		return nil, "", fmt.Errorf("unexpected content type %q", contentType)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", fmt.Errorf("media is larger than %d bytes", limit)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty media body")
	}
	return data, contentType, nil
}

func extensionFor(contentType, mediaURL string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	}

	ext := mediaExtension(mediaURL)
	if ext != "" {
		return ext
	}
	return ".bin"
}

// mediaExtension returns the URL's file extension, ignoring its query string
// so signed CDN links still keep a useful attachment name.
func mediaExtension(mediaURL string) string {
	u, err := url.Parse(mediaURL)
	if err != nil {
		return ""
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".mp4", ".webm", ".mov":
		return ext
	}
	return ""
}
