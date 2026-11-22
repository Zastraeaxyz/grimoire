package stats

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestBlogSlug(t *testing.T) {
	cases := []struct {
		path string
		slug string
		ok   bool
	}{
		{"/zastraeachastity", "zastraeachastity", true},
		{"/Some_Blog-1", "Some_Blog-1", true},
		{"/zastraeachastity/archive", "zastraeachastity", true},
		{"/zastraeachastity/follower-feed", "zastraeachastity", true},
		{"/zastraeachastity/unknown-tab", "", false},
		{"/a/b/c", "", false},
		{"/", "", false},
		{"", "", false},
		{"/home", "", false},
		{"/Home", "", false},
		{"/login", "", false},
		{"/tag/something", "", false},
		{"/post", "", false},
		{"/v2", "", false},
		{"/blog search", "", false},
	}
	for _, tc := range cases {
		slug, ok := blogSlug(tc.path)
		if slug != tc.slug || ok != tc.ok {
			t.Errorf("blogSlug(%q) = (%q, %v), want (%q, %v)", tc.path, slug, ok, tc.slug, tc.ok)
		}
	}
}

func TestSplitPathsAggregatesBlogs(t *testing.T) {
	blogs, pages := splitPaths([]Metric{
		{X: "/zastraeachastity", Y: 60},
		{X: "/zastraeachastity/archive", Y: 5},
		{X: "/Dogtanian69", Y: 33},
		{X: "/home", Y: 162},
		{X: "/", Y: 206},
		{X: "/login", Y: 164},
	})

	if len(blogs) != 2 {
		t.Fatalf("blogs = %+v", blogs)
	}
	if blogs[0].X != "zastraeachastity" || blogs[0].Y != 65 {
		t.Fatalf("first blog = %+v", blogs[0])
	}
	if blogs[1].X != "Dogtanian69" || blogs[1].Y != 33 {
		t.Fatalf("second blog = %+v", blogs[1])
	}

	wantPages := []string{"/", "/login", "/home"}
	if len(pages) != len(wantPages) {
		t.Fatalf("pages = %+v", pages)
	}
	for i, want := range wantPages {
		if pages[i].X != want {
			t.Errorf("pages[%d] = %q, want %q", i, pages[i].X, want)
		}
	}
}

func TestBuildEmbeds(t *testing.T) {
	rep := &report{
		now:   time.Date(2026, time.November, 22, 9, 0, 0, 0, time.UTC),
		loc:   time.UTC,
		today: &WebsiteStats{Visitors: 12, Pageviews: 145},
		yesterday: &WebsiteStats{
			Visitors: 80, Pageviews: 511, Visits: 107, Bounces: 14, TotalTime: 39916,
		},
		week: &WebsiteStats{Visitors: 411, Pageviews: 3532, Visits: 663},
		topBlogs: []Metric{
			{X: "zastraeachastity", Y: 60},
			{X: "Dogtanian69", Y: 33},
		},
	}
	rep.yesterday.Comparison.Visitors = 40

	embeds := buildEmbeds(rep, "https://bdsmlr.zastraea.xyz/")
	if len(embeds) != 1 {
		t.Fatalf("embeds = %d, want 1", len(embeds))
	}

	summary := embeds[0]
	if !strings.Contains(summary.Title, "Sun 22 Nov 2026") {
		t.Errorf("title = %q", summary.Title)
	}
	values := fieldValues(summary)
	if !strings.Contains(values, "**12** visitors") {
		t.Errorf("missing today visitors: %q", values)
	}
	if !strings.Contains(values, "**80** visitors") {
		t.Errorf("missing yesterday visitors: %q", values)
	}
	if !strings.Contains(values, "+100% vs day before") {
		t.Errorf("missing visitor delta: %q", values)
	}
	if !strings.Contains(values, "Average visit 6m 13s") {
		t.Errorf("missing average visit: %q", values)
	}
	if !strings.Contains(values, "Bounce rate 13%") {
		t.Errorf("missing bounce rate: %q", values)
	}
	if !strings.Contains(values, "[zastraeachastity](https://bdsmlr.zastraea.xyz/zastraeachastity) · 60") {
		t.Errorf("missing blog link: %q", values)
	}
	if !strings.Contains(values, "[Dogtanian69](https://bdsmlr.zastraea.xyz/Dogtanian69) · 33") {
		t.Errorf("missing second blog: %q", values)
	}
}

func TestBuildEmbedsWithNoData(t *testing.T) {
	rep := &report{
		now:       time.Date(2026, time.November, 22, 9, 0, 0, 0, time.UTC),
		loc:       time.UTC,
		today:     &WebsiteStats{},
		yesterday: &WebsiteStats{},
		week:      &WebsiteStats{},
	}
	embeds := buildEmbeds(rep, defaultSiteURL)
	if len(embeds) != 1 {
		t.Fatalf("embeds = %d", len(embeds))
	}
	values := fieldValues(embeds[0])
	if strings.Count(values, "No data yet.") != 1 {
		t.Fatalf("empty fields = %q", values)
	}
	if !strings.Contains(values, "no earlier data") {
		t.Fatalf("missing empty delta: %q", values)
	}
}

func TestDeltaText(t *testing.T) {
	cases := []struct {
		current, previous int64
		want              string
	}{
		{80, 40, "+100% vs day before"},
		{40, 80, "-50% vs day before"},
		{10, 0, "no earlier data"},
	}
	for _, tc := range cases {
		if got := deltaText(tc.current, tc.previous); got != tc.want {
			t.Errorf("deltaText(%d, %d) = %q, want %q", tc.current, tc.previous, got, tc.want)
		}
	}
}

func TestGroupThousands(t *testing.T) {
	cases := map[string]string{
		"0":       "0",
		"7":       "7",
		"999":     "999",
		"1000":    "1,000",
		"3532":    "3,532",
		"1234567": "1,234,567",
		"-1234":   "-1,234",
	}
	for input, want := range cases {
		if got := groupThousands(input); got != want {
			t.Errorf("groupThousands(%q) = %q, want %q", input, got, want)
		}
	}
}

func fieldValues(embed *discordgo.MessageEmbed) string {
	var out strings.Builder
	for _, field := range embed.Fields {
		out.WriteString(field.Name)
		out.WriteString("\n")
		out.WriteString(field.Value)
		out.WriteString("\n")
	}
	return out.String()
}
