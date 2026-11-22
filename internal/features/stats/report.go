package stats

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	// metricsLimit is generous because Umami ranks paths, and the dashboard
	// needs every blog path before it can take the top five.
	metricsLimit = 5000

	rankedLimit = 5
	embedColor  = 0x7C3AED
	fieldLimit  = 1024
)

// report is everything one dashboard refresh shows.
type report struct {
	now time.Time
	loc *time.Location

	today     *WebsiteStats
	yesterday *WebsiteStats
	week      *WebsiteStats

	topBlogs []Metric
}

// collect reads the three summary windows and the last-24-hours rankings. The
// requests run together so a slow Umami response cannot spend the interaction
// budget nine times over.
func (f *Feature) collect(ctx context.Context, now time.Time) (*report, error) {
	loc := f.loc
	local := now.In(loc)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	yesterdayStart := dayStart.AddDate(0, 0, -1)
	weekStart := dayStart.AddDate(0, 0, -6)
	rankStart := now.Add(-24 * time.Hour)

	rep := &report{now: now, loc: loc}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}

	run(func() (err error) {
		rep.today, err = f.umami.stats(ctx, dayStart, now)
		return err
	})
	run(func() (err error) {
		rep.yesterday, err = f.umami.stats(ctx, yesterdayStart, dayStart)
		return err
	})
	run(func() (err error) {
		rep.week, err = f.umami.stats(ctx, weekStart, now)
		return err
	})
	run(func() error {
		paths, err := f.umami.metrics(ctx, "path", rankStart, now, metricsLimit)
		if err != nil {
			return err
		}
		rep.topBlogs, _ = splitPaths(paths)
		return nil
	})

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return rep, nil
}

// Static pages bbdsmlr tracks alongside blog profiles. The list mirrors the
// reserved route names in bbdsmlr's src/lib/analytics.ts, so a page is never
// mistaken for a blog.
var staticPaths = map[string]struct{}{
	"about":          {},
	"activity":       {},
	"api":            {},
	"assets":         {},
	"blocked":        {},
	"blog-search":    {},
	"changelog":      {},
	"compose":        {},
	"customize":      {},
	"drafts":         {},
	"favicon.ico":    {},
	"favicon.svg":    {},
	"following":      {},
	"for-you":        {},
	"home":           {},
	"icons.svg":      {},
	"invitations":    {},
	"liked":          {},
	"login":          {},
	"messages":       {},
	"og-default.png": {},
	"post":           {},
	"previews":       {},
	"queue":          {},
	"search":         {},
	"settings":       {},
	"tag":            {},
	"v2":             {},
}

// Blog tab pages collapse to the blog path in bbdsmlr's tracker, but older
// data may still carry them.
var blogTabs = map[string]struct{}{
	"activity":      {},
	"archive":       {},
	"follower-feed": {},
	"followers":     {},
	"following":     {},
	"siblings":      {},
}

var blogSlugPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// splitPaths separates blog profiles from the app's own pages and sums each
// blog into one row.
func splitPaths(paths []Metric) (blogs, pages []Metric) {
	totals := make(map[string]float64)
	for _, row := range paths {
		if slug, ok := blogSlug(row.X); ok {
			totals[slug] += row.Y
			continue
		}
		pages = append(pages, row)
	}
	blogs = make([]Metric, 0, len(totals))
	for slug, count := range totals {
		blogs = append(blogs, Metric{X: slug, Y: count})
	}
	sortMetrics(blogs)
	sortMetrics(pages)
	return blogs, pages
}

// blogSlug reports whether a tracked path is a blog profile, optionally with
// one of bbdsmlr's profile tabs.
func blogSlug(path string) (string, bool) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) > 2 {
		return "", false
	}
	name := parts[0]
	if _, reserved := staticPaths[strings.ToLower(name)]; reserved {
		return "", false
	}
	if !blogSlugPattern.MatchString(name) {
		return "", false
	}
	if len(parts) == 2 {
		if _, tab := blogTabs[strings.ToLower(parts[1])]; !tab {
			return "", false
		}
	}
	return name, true
}

func sortMetrics(rows []Metric) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Y == rows[j].Y {
			return rows[i].X < rows[j].X
		}
		return rows[i].Y > rows[j].Y
	})
}

// buildEmbeds renders one refresh as a compact dashboard: the summary numbers
// and the most visited blogs.
func buildEmbeds(rep *report, siteURL string) []*discordgo.MessageEmbed {
	now := rep.now
	return []*discordgo.MessageEmbed{
		{
			Title:       "bbdsmlr · " + now.In(rep.loc).Format("Mon 2 Jan 2006"),
			Description: "Anonymous, cookieless Umami analytics for bdsmlr.zastraea.xyz.",
			Color:       embedColor,
			Fields: []*discordgo.MessageEmbedField{
				{
					Name: "Today so far",
					Value: fmt.Sprintf("**%s** visitors\n**%s** pageviews",
						formatCount(rep.today.Visitors), formatCount(rep.today.Pageviews)),
					Inline: true,
				},
				{
					Name: "Yesterday",
					Value: fmt.Sprintf("**%s** visitors\n**%s** pageviews\n%s",
						formatCount(rep.yesterday.Visitors),
						formatCount(rep.yesterday.Pageviews),
						deltaText(rep.yesterday.Visitors, rep.yesterday.Comparison.Visitors)),
					Inline: true,
				},
				{
					Name: "Last 7 days",
					Value: fmt.Sprintf("**%s** visitors\n**%s** pageviews\n**%s** visits",
						formatCount(rep.week.Visitors),
						formatCount(rep.week.Pageviews),
						formatCount(rep.week.Visits)),
					Inline: true,
				},
				{
					Name:   "Yesterday engagement",
					Value:  engagementText(rep.yesterday),
					Inline: true,
				},
				{
					Name:  "Top blogs (last 24 hours)",
					Value: metricField(rep.topBlogs, rankedLimit, func(row Metric) string { return blogLine(row, siteURL) }),
				},
			},
			Footer:    &discordgo.MessageEmbedFooter{Text: "Umami · updated"},
			Timestamp: now.UTC().Format(time.RFC3339),
		},
	}
}

func metricField(rows []Metric, limit int, render func(Metric) string) string {
	if limit > len(rows) {
		limit = len(rows)
	}
	if limit == 0 {
		return "No data yet."
	}
	lines := make([]string, 0, limit)
	for _, row := range rows[:limit] {
		lines = append(lines, render(row))
	}
	return truncate(strings.Join(lines, "\n"), fieldLimit)
}

func blogLine(row Metric, siteURL string) string {
	base := strings.TrimRight(siteURL, "/")
	slug := row.X
	label := strings.NewReplacer("[", "", "]", "").Replace(slug)
	link := base + "/" + url.PathEscape(slug)
	return fmt.Sprintf("[%s](%s) · %s", truncate(label, 60), link, formatFloat(row.Y))
}

func deltaText(current, previous int64) string {
	if previous <= 0 {
		return "no earlier data"
	}
	pct := float64(current-previous) / float64(previous) * 100
	return fmt.Sprintf("%+.0f%% vs day before", pct)
}

func engagementText(window *WebsiteStats) string {
	avg := "no visits"
	if window.Visits > 0 {
		avg = formatDuration(time.Duration(float64(window.TotalTime)/float64(window.Visits)) * time.Second)
	}
	bounce := 0
	if window.Visits > 0 {
		bounce = int(math.Round(float64(window.Bounces) / float64(window.Visits) * 100))
	}
	return fmt.Sprintf("Average visit %s\nBounce rate %d%%", avg, bounce)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatCount(n int64) string {
	return groupThousands(strconv.FormatInt(n, 10))
}

func formatFloat(n float64) string {
	return groupThousands(strconv.FormatInt(int64(math.Round(n)), 10))
}

func groupThousands(digits string) string {
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign = "-"
		digits = digits[1:]
	}
	if len(digits) <= 3 {
		return sign + digits
	}
	var out strings.Builder
	first := len(digits) % 3
	if first > 0 {
		out.WriteString(digits[:first])
	}
	for i := first; i < len(digits); i += 3 {
		if out.Len() > 0 {
			out.WriteByte(',')
		}
		out.WriteString(digits[i : i+3])
	}
	return sign + out.String()
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
