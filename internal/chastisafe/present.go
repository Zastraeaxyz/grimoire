package chastisafe

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// LockeesText summarizes every active lockee.
func LockeesText(locks []LoadedLock) string {
	if len(locks) == 0 {
		return "No active lockees."
	}

	sorted := append([]LoadedLock(nil), locks...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := lockUrgency(&sorted[i]), lockUrgency(&sorted[j])
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
	fmt.Fprintf(&b, "**%d active lockee%s**\n", len(sorted), PluralSuffix(len(sorted)))
	fmt.Fprintf(&b, "Frozen: %d · Ready: %d · Check-ins due within an hour: %d\n", frozen, ready, dueSoon)
	for i := range sorted {
		b.WriteString("\n")
		b.WriteString(LockeeLine(&sorted[i]))
	}
	return b.String()
}

// LockeeLine renders one lockee's running lock.
func LockeeLine(lock *LoadedLock) string {
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
		name, lockName, LockTypeLabel(lock.Type, lock.IsCardLock(), lock.IsTimerLock()), LockStateText(lock))
}

// PersonalStatusText shows the caller's own locks and filters out locks they
// only hold as a keyholder.
func PersonalStatusText(locks []LoadedLock, chastisafeID string) string {
	own := make([]LoadedLock, 0, len(locks))
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
	fmt.Fprintf(&b, "**Your active lock%s**\n", PluralSuffix(len(own)))
	for i := range own {
		b.WriteString("\n")
		b.WriteString(PersonalLockLine(&own[i]))
	}
	return b.String()
}

// PersonalLockLine renders one of the caller's own locks.
func PersonalLockLine(lock *LoadedLock) string {
	if lock == nil {
		return ""
	}
	lockName := strings.TrimSpace(lock.LockName)
	if lockName == "" {
		lockName = "lock"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "• **%s** (%s)", lockName,
		LockTypeLabel(lock.Type, lock.IsCardLock(), lock.IsTimerLock()))
	if lock.Keyholder != "" {
		if lock.IsSelfLock() {
			b.WriteString(": self-locked")
		} else {
			fmt.Fprintf(&b, ": held by %s", lock.Keyholder)
		}
	}
	b.WriteString(", ")
	b.WriteString(LockStateText(lock))
	return b.String()
}

// LockStateText is a short human-readable state.
func LockStateText(lock *LoadedLock) string {
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
		parts = append(parts, FormatShortDuration(lock.Remaining())+" left")
	}
	if lock.IsCardLock() {
		if cards := lock.CardsRemaining(); cards > 0 {
			parts = append(parts, fmt.Sprintf("%d card%s left", cards, PluralSuffix(cards)))
		}
		if greens := lock.GreensRemaining(); greens > 0 {
			parts = append(parts, fmt.Sprintf("%d green%s needed", greens, PluralSuffix(greens)))
		}
	}
	if next := lock.NextCheckInIn(); next > 0 {
		parts = append(parts, "check-in in "+FormatShortDuration(next))
	} else if lock.CheckInPeriod > 0 {
		parts = append(parts, "check-in due")
	}
	if hygiene := lock.NextHygieneIn(); hygiene > 0 {
		parts = append(parts, "hygiene in "+FormatShortDuration(hygiene))
	}
	if len(parts) == 0 {
		parts = append(parts, lock.Status())
	}
	return strings.Join(parts, ", ")
}

func lockUrgency(lock *LoadedLock) int {
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

// LocksText lists templates by popularity, which ChastiSafe exposes as the
// number of lockees currently using each template.
func LocksText(templates []Template) string {
	if len(templates) == 0 {
		return "You have no lock templates."
	}

	sorted := append([]Template(nil), templates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Lockees != sorted[j].Lockees {
			return sorted[i].Lockees > sorted[j].Lockees
		}
		return strings.ToLower(sorted[i].LockName) < strings.ToLower(sorted[j].LockName)
	})

	var b strings.Builder
	fmt.Fprintf(&b, "**%d lock template%s**, most popular first\n", len(sorted), PluralSuffix(len(sorted)))
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
			LockTypeLabel(sorted[i].LockType, sorted[i].IsCardLock(), sorted[i].IsTimerLock()),
			sorted[i].Lockees,
			PluralSuffix(sorted[i].Lockees),
			status,
		)
	}
	return b.String()
}

// LockTypeLabel names a lock or template shape.
func LockTypeLabel(raw string, card, timer bool) string {
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

// ProfileText renders a public ChastiSafe profile with both sides of the
// account: what it does as a lockee and what it does as a keyholder.
func ProfileText(profile *Profile) string {
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

func countSummary(stats Stats) string {
	parts := make([]string, 0, len(countStatLabels))
	for _, label := range countStatLabels {
		value := stats.Value(label.key)
		if value <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s%s", value, label.noun, PluralSuffix(value)))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func durationSummary(stats Stats) string {
	parts := make([]string, 0, len(durationStatLabels))
	for _, label := range durationStatLabels {
		value := stats.Value(label.key)
		if value <= 0 {
			continue
		}
		parts = append(parts, FormatShortDuration(time.Duration(value)*time.Second)+" "+label.noun)
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
	return fmt.Sprintf("%.1f / 5 from %d rating%s", average, count, PluralSuffix(count))
}

func levelSummary(levels Levels) string {
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

func currentLockText(entries []LockInfoEntry) string {
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

// PluralSuffix returns an s for counts other than one.
func PluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// TemplateDetailText renders one template. Passcodes are never included.
func TemplateDetailText(template *Template) string {
	if template == nil {
		return "Template not found."
	}
	name := strings.TrimSpace(template.LockName)
	if name == "" {
		name = "Untitled lock"
	}
	status := strings.ToUpper(strings.TrimSpace(template.Status))
	if status == "" {
		status = "UNKNOWN"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%s** (ID `%s`)\n", name, template.ID)
	fmt.Fprintf(&b, "Type: %s · Status: %s · Lockees: %d\n",
		LockTypeLabel(template.LockType, template.IsCardLock(), template.IsTimerLock()),
		status,
		template.Lockees,
	)
	fmt.Fprintf(&b, "Hidden: %s · Start frozen: %s · Allow updates: %t\n",
		displayYesNo(template.Hidden), displayYesNo(template.StartFrozen), template.AllowUpdates)
	if description := strings.TrimSpace(template.Description); description != "" {
		fmt.Fprintf(&b, "Description: %s\n", description)
	}
	if template.IsTimerLock() {
		fmt.Fprintf(&b, "Timer: %s to %s\n",
			FormatDuration(time.Duration(template.TimerMin)*time.Second),
			FormatDuration(time.Duration(template.TimerMax)*time.Second),
		)
	}
	if template.IsCardLock() {
		fmt.Fprintf(&b, "Cards: %s\n", templateCardsText(template.Cards))
		fmt.Fprintf(&b, "Regularity: %s\n", FormatMinutes(template.Regularity))
	}
	if template.CheckInPeriod > 0 {
		fmt.Fprintf(&b, "Check-in: every %s, tolerance %s\n",
			FormatMinutes(template.CheckInPeriod), FormatMinutes(template.CheckInTolerance))
	}
	if template.HygieneUnlockPeriod > 0 {
		fmt.Fprintf(&b, "Hygiene: every %s for %s\n",
			FormatMinutes(template.HygieneUnlockPeriod), FormatMinutes(template.HygieneUnlockDuration))
	}
	if template.AutoResetPeriod > 0 {
		fmt.Fprintf(&b, "Auto reset: every %s, %d time%s\n",
			FormatMinutes(template.AutoResetPeriod), template.AutoResetCount, PluralSuffix(template.AutoResetCount))
	}
	fmt.Fprintf(&b, "Allowed purposes: %s\n", FormatPurposes(template.AllowedPurposes))
	if template.Passcode != "" {
		b.WriteString("Passcode: set\n")
	} else {
		b.WriteString("Passcode: not set\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func templateCardsText(cards []TemplateCard) string {
	if len(cards) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(cards))
	for _, card := range cards {
		parts = append(parts, fmt.Sprintf("%s %d-%d", card.Name.Short(), card.Min, card.Max))
	}
	return strings.Join(parts, ", ")
}

// LockListText renders running locks with the caller's role.
func LockListText(views []LockView) string {
	if len(views) == 0 {
		return "No matching running locks."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**%d running lock%s**\n", len(views), PluralSuffix(len(views)))
	for i := range views {
		lock := &views[i].Lock
		name := strings.TrimSpace(lock.LockName)
		if name == "" {
			name = "lock"
		}
		lockee := strings.TrimSpace(lock.Lockee)
		if lockee == "" {
			lockee = "unknown lockee"
		}
		keyholder := strings.TrimSpace(lock.Keyholder)
		if keyholder == "" {
			keyholder = "unknown keyholder"
		}
		fmt.Fprintf(&b, "\n• **%s** (%s) · %s · keyholder %s · role %s\n  %s",
			name,
			LockTypeLabel(lock.Type, lock.IsCardLock(), lock.IsTimerLock()),
			lockee,
			keyholder,
			roleText(views[i].Role),
			LockStateText(lock),
		)
	}
	return b.String()
}

// LockDetailText renders one running lock without combinations or codes.
func LockDetailText(view *LockView) string {
	if view == nil {
		return "Lock not found."
	}
	lock := &view.Lock
	name := strings.TrimSpace(lock.LockName)
	if name == "" {
		name = "lock"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**%s** (ID `%s`)\n", name, lock.ID)
	fmt.Fprintf(&b, "Type: %s · Role: %s · Purpose: %s\n",
		LockTypeLabel(lock.Type, lock.IsCardLock(), lock.IsTimerLock()),
		roleText(view.Role),
		displayPurpose(lock.LockPurpose),
	)
	fmt.Fprintf(&b, "Lockee: %s · Keyholder: %s\n", displayName(lock.Lockee), displayName(lock.Keyholder))
	fmt.Fprintf(&b, "State: %s", LockStateText(lock))
	if lock.Hidden {
		b.WriteString(" · hidden")
	}
	if !lock.LoadTime.IsZero() {
		fmt.Fprintf(&b, " · loaded <t:%d:R>", lock.LoadTime.Unix())
	}
	b.WriteString("\n")
	if lock.IsTimerLock() {
		fmt.Fprintf(&b, "Remaining: %s\n", FormatDuration(lock.Remaining()))
	}
	if lock.IsCardLock() {
		fmt.Fprintf(&b, "Cards: %s\n", DeckSummary(lock.Deck))
		fmt.Fprintf(&b, "Picks: %d", lock.Picks)
		if lock.MaxPicks > 0 {
			fmt.Fprintf(&b, "/%d", lock.MaxPicks)
		}
		b.WriteString("\n")
	}
	if lock.CheckInPeriod > 0 {
		fmt.Fprintf(&b, "Check-in: every %s, %s\n",
			FormatMinutes(lock.CheckInPeriod),
			nextInText(lock.NextCheckInIn()),
		)
	}
	if lock.TimeToHygieneUnlock > 0 {
		fmt.Fprintf(&b, "Hygiene unlock in %s\n", FormatShortDuration(lock.NextHygieneIn()))
	}
	if lock.AutoResetsRemaining > 0 {
		fmt.Fprintf(&b, "Auto resets remaining: %d\n", lock.AutoResetsRemaining)
	}
	if lock.TaskRollerID != "" {
		fmt.Fprintf(&b, "Task roller: `%s`\n", lock.TaskRollerID)
	}
	if lock.PilloryRemaining > 0 {
		fmt.Fprintf(&b, "Pillory votes remaining: %d\n", lock.PilloryRemaining)
	}
	if len(lock.Buttons) > 0 {
		names := make([]string, 0, len(lock.Buttons))
		for _, button := range lock.Buttons {
			if text := strings.TrimSpace(button.ButtonText); text != "" {
				names = append(names, text)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "Buttons: %s\n", strings.Join(names, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func roleText(role LockRole) string {
	switch role {
	case RoleLockee:
		return "lockee"
	case RoleKeyholder:
		return "keyholder"
	case RoleBoth:
		return "lockee and keyholder"
	default:
		return "observer"
	}
}

func displayPurpose(purpose Purpose) string {
	value := strings.TrimSpace(string(purpose))
	if value == "" {
		return "unknown"
	}
	return value
}

func displayName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func displayYesNo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func nextInText(duration time.Duration) string {
	if duration > 0 {
		return "next in " + FormatShortDuration(duration)
	}
	return "due now"
}
