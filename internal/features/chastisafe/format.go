package chastisafe

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"grimoire/internal/bot"
	cs "grimoire/internal/chastisafe"
)

const messageChunkLimit = 1900

// lockeesText summarizes every active lockee.
func lockeesText(locks []cs.LoadedLock) string {
	if len(locks) == 0 {
		return "No active lockees."
	}

	sorted := append([]cs.LoadedLock(nil), locks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := urgency(&sorted[i]), urgency(&sorted[j])
		if left != right {
			return left < right
		}
		leftName := strings.ToLower(strings.TrimSpace(sorted[i].Lockee))
		rightName := strings.ToLower(strings.TrimSpace(sorted[j].Lockee))
		if leftName != rightName {
			return leftName < rightName
		}
		return strings.ToLower(sorted[i].LockName) < strings.ToLower(sorted[j].LockName)
	})

	frozen, ready, dueSoon := 0, 0, 0
	for i := range sorted {
		if sorted[i].IsFrozen() {
			frozen++
		}
		if sorted[i].IsReady() {
			ready++
		}
		if next := sorted[i].NextCheckInIn(); next > 0 && next <= time.Hour {
			dueSoon++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%d active lockee%s**\n", len(sorted), pluralSuffix(len(sorted)))
	fmt.Fprintf(&b, "Frozen: %d · Ready: %d · Check-ins due within an hour: %d\n", frozen, ready, dueSoon)
	for i := range sorted {
		b.WriteString("\n")
		b.WriteString(lockeeLine(&sorted[i]))
	}
	return b.String()
}

func lockeeLine(lock *cs.LoadedLock) string {
	if lock == nil {
		return ""
	}
	name := strings.TrimSpace(lock.Lockee)
	if name == "" {
		name = "Unknown lockee"
	}
	lockName := strings.TrimSpace(lock.LockName)
	if lockName == "" {
		lockName = "lock"
	}
	return fmt.Sprintf("• **%s** · %s (%s): %s",
		name, lockName, lockTypeLabel(lock.Type, lock.IsCardLock(), lock.IsTimerLock()), lockState(lock))
}

// personalStatusText shows the caller's own locks and filters out locks they
// only hold as a keyholder.
func personalStatusText(locks []cs.LoadedLock, chastisafeID string) string {
	own := make([]cs.LoadedLock, 0, len(locks))
	for i := range locks {
		if chastisafeID != "" && locks[i].LockeeID.String() != chastisafeID {
			continue
		}
		own = append(own, locks[i])
	}
	if len(own) == 0 {
		return "You have no active ChastiSafe locks."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**Your active lock%s**\n", pluralSuffix(len(own)))
	for i := range own {
		b.WriteString("\n")
		b.WriteString(personalLockLine(&own[i]))
	}
	return b.String()
}

func personalLockLine(lock *cs.LoadedLock) string {
	if lock == nil {
		return ""
	}
	lockName := strings.TrimSpace(lock.LockName)
	if lockName == "" {
		lockName = "lock"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "• **%s** (%s)", lockName,
		lockTypeLabel(lock.Type, lock.IsCardLock(), lock.IsTimerLock()))
	if lock.Keyholder != "" {
		if lock.IsSelfLock() {
			b.WriteString(": self-locked")
		} else {
			fmt.Fprintf(&b, ": held by %s", lock.Keyholder)
		}
	}
	b.WriteString(", ")
	b.WriteString(lockState(lock))
	return b.String()
}

// lockState is the short human-readable state shared by both views.
func lockState(lock *cs.LoadedLock) string {
	if lock == nil {
		return ""
	}

	parts := make([]string, 0, 4)
	if lock.IsFrozen() {
		parts = append(parts, "frozen")
	}
	if lock.IsReady() {
		parts = append(parts, "ready to unlock")
	}
	if lock.IsTimerLock() && !lock.IsFrozen() {
		parts = append(parts, cs.FormatShortDuration(lock.Remaining())+" left")
	}
	if lock.IsCardLock() {
		if cards := lock.CardsRemaining(); cards > 0 {
			parts = append(parts, fmt.Sprintf("%d card%s left", cards, pluralSuffix(cards)))
		}
		if greens := lock.GreensRemaining(); greens > 0 {
			parts = append(parts, fmt.Sprintf("%d green%s needed", greens, pluralSuffix(greens)))
		}
	}
	if next := lock.NextCheckInIn(); next > 0 {
		parts = append(parts, "check-in in "+cs.FormatShortDuration(next))
	} else if lock.CheckInPeriod > 0 {
		parts = append(parts, "check-in due")
	}
	if hygiene := lock.NextHygieneIn(); hygiene > 0 {
		parts = append(parts, "hygiene in "+cs.FormatShortDuration(hygiene))
	}
	if len(parts) == 0 {
		parts = append(parts, lock.Status())
	}
	return strings.Join(parts, ", ")
}

// urgency orders lockees by what needs attention first.
func urgency(lock *cs.LoadedLock) int {
	if lock == nil {
		return 99
	}
	switch {
	case lock.IsReady():
		return 0
	case lock.IsFrozen():
		return 1
	case lock.NextCheckInIn() > 0 && lock.NextCheckInIn() <= time.Hour:
		return 2
	case lock.IsTimerLock() && lock.Remaining() <= time.Hour:
		return 3
	case lock.IsTimerLock():
		return 4
	default:
		return 5
	}
}

// locksText lists templates by popularity, which ChastiSafe exposes as the
// number of lockees currently using each template.
func locksText(templates []cs.Template) string {
	if len(templates) == 0 {
		return "You have no lock templates."
	}

	sorted := append([]cs.Template(nil), templates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Lockees != sorted[j].Lockees {
			return sorted[i].Lockees > sorted[j].Lockees
		}
		return strings.ToLower(sorted[i].LockName) < strings.ToLower(sorted[j].LockName)
	})

	var b strings.Builder
	fmt.Fprintf(&b, "**%d lock template%s**, most popular first\n", len(sorted), pluralSuffix(len(sorted)))
	for i := range sorted {
		lockName := strings.TrimSpace(sorted[i].LockName)
		if lockName == "" {
			lockName = "Untitled lock"
		}
		status := strings.ToLower(strings.TrimSpace(sorted[i].Status))
		if status == "" {
			status = "enabled"
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "%d. **%s** · %s · %d lockee%s · %s",
			i+1,
			lockName,
			lockTypeLabel(sorted[i].LockType, sorted[i].IsCardLock(), sorted[i].IsTimerLock()),
			sorted[i].Lockees,
			pluralSuffix(sorted[i].Lockees),
			status,
		)
	}
	return b.String()
}

func lockTypeLabel(raw string, card, timer bool) string {
	switch {
	case card:
		return "card"
	case timer:
		return "timer"
	}
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "lock"
	}
	return value
}

// profileText renders a public ChastiSafe profile with both sides of the
// account: what it does as a lockee and what it does as a keyholder.
func profileText(profile *cs.Profile) string {
	if profile == nil {
		return "No ChastiSafe profile found."
	}

	name := strings.TrimSpace(profile.User)
	if name == "" {
		name = "Unknown account"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%s** (ID `%s`)\n", name, profile.ID)
	if profile.Visible {
		b.WriteString("Public profile")
	} else {
		b.WriteString("Private profile")
	}

	b.WriteString("\n\n**Lockee stats**\n")
	fmt.Fprintf(&b, "Locks: %s\n", countSummary(profile.SelfLockCounts))
	fmt.Fprintf(&b, "Time: %s\n", durationSummary(profile.SelfStats))
	fmt.Fprintf(&b, "Rating: %s\n", ratingSummary(
		profile.Ratings.AverageRatingAsLockee,
		profile.Ratings.RatingsAsLockeeCount,
	))
	if levels := levelSummary(profile.Levels); levels != "" {
		fmt.Fprintf(&b, "Level: %s\n", levels)
	}

	b.WriteString("\n**Keyholder stats**\n")
	fmt.Fprintf(&b, "Locks held: %s\n", countSummary(profile.KeyholderLockCounts))
	fmt.Fprintf(&b, "Time: %s\n", durationSummary(profile.KeyholderStats))
	fmt.Fprintf(&b, "Rating: %s\n", ratingSummary(
		profile.Ratings.AverageRatingAsKeyholder,
		profile.Ratings.RatingsAsKeyholderCount,
	))

	if locks := currentLockText(profile.LockInfo.ChastityLocks); locks != "" {
		b.WriteString("\n**Current locks**\n")
		b.WriteString(locks)
	}
	return b.String()
}

type statLabel struct {
	key  string
	noun string
}

var countStatLabels = []statLabel{
	{key: "CHASTITY", noun: "chastity lock"},
	{key: "BONDAGE", noun: "bondage lock"},
	{key: "TASK", noun: "task lock"},
	{key: "LOYALTY", noun: "loyalty lock"},
	{key: "EMERGENCY", noun: "emergency lock"},
}

var durationStatLabels = []statLabel{
	{key: "CHASTITY", noun: "chastity"},
	{key: "CHASTITY-LONGEST", noun: "longest chastity"},
	{key: "BONDAGE", noun: "bondage"},
	{key: "TASK", noun: "task"},
	{key: "LOYALTY", noun: "loyalty"},
	{key: "EMERGENCY", noun: "emergency"},
}

func countSummary(stats cs.Stats) string {
	parts := make([]string, 0, len(countStatLabels))
	for _, label := range countStatLabels {
		value := stats.Value(label.key)
		if value <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s%s", value, label.noun, pluralSuffix(value)))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func durationSummary(stats cs.Stats) string {
	parts := make([]string, 0, len(durationStatLabels))
	for _, label := range durationStatLabels {
		value := stats.Value(label.key)
		if value <= 0 {
			continue
		}
		parts = append(parts, cs.FormatShortDuration(time.Duration(value)*time.Second)+" "+label.noun)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func ratingSummary(average float64, count int) string {
	if count <= 0 {
		return "no ratings yet"
	}
	return fmt.Sprintf("%.1f / 5 from %d rating%s", average, count, pluralSuffix(count))
}

func levelSummary(levels cs.Levels) string {
	parts := make([]string, 0, 3)
	for _, level := range []struct {
		label string
		value string
	}{
		{label: "chastity", value: levels.ChastityLevel},
		{label: "bondage", value: levels.BondageLevel},
		{label: "task", value: levels.TaskLevel},
	} {
		value := strings.TrimSpace(level.value)
		if value == "" {
			continue
		}
		parts = append(parts, level.label+" "+value)
	}
	return strings.Join(parts, ", ")
}

func currentLockText(entries []cs.LockInfoEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for i, entry := range entries {
		if i > 0 {
			b.WriteString("\n")
		}
		name := strings.TrimSpace(entry.LockName)
		if name == "" {
			name = "chastity lock"
		}
		fmt.Fprintf(&b, "• %s", name)
		if keyholder := strings.TrimSpace(entry.Keyholder); keyholder != "" {
			fmt.Fprintf(&b, ", held by %s", keyholder)
		}
		if !entry.LoadTime.IsZero() {
			fmt.Fprintf(&b, " since <t:%d:R>", entry.LoadTime.Unix())
		}
	}
	return b.String()
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

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
