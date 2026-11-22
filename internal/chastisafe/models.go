package chastisafe

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Status is the API's frequently inconsistent status value. It accepts 0,
// false, "0" and "false" as success.
type Status int

const (
	StatusOK    Status = 0
	StatusError Status = 1
)

func (s *Status) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*s = StatusOK
		return nil
	}
	text = strings.Trim(text, `"`)
	switch strings.ToLower(text) {
	case "", "0", "false":
		*s = StatusOK
	case "1", "true":
		*s = StatusError
	default:
		value, err := strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("chastisafe: %q is not a status", text)
		}
		*s = Status(value)
	}
	return nil
}

func (s Status) OK() bool { return s == StatusOK }

// Identity is the response from /who.
type Identity struct {
	Status      Status `json:"status"`
	User        string `json:"user"`
	ID          ID     `json:"id"`
	DisplayMode int    `json:"displayMode"`
}

// OwnedBy reports whether the account is linked to a Discord snowflake.
func (i Identity) OwnedBy(discordID string) bool {
	return i.ID != "" && i.ID.String() == strings.TrimSpace(discordID)
}

// Profile is a ChastiSafe account profile.
type Profile struct {
	User                       string         `json:"user"`
	DisplayMode                int            `json:"displayMode"`
	ShowCombinationToKeyholder bool           `json:"showCombinationToKeyholder"`
	Digits                     int            `json:"digits"`
	CharacterSet               string         `json:"characterSet"`
	ExpandLockees              bool           `json:"expandLockees"`
	Visible                    bool           `json:"visible"`
	Roles                      int            `json:"roles"`
	Description                string         `json:"description"`
	Stats                      Stats          `json:"stats"`
	SelfStats                  Stats          `json:"selfStats"`
	KeyholderStats             Stats          `json:"keyholderStats"`
	KeyholderLockCounts        Stats          `json:"keyholderLockCounts"`
	SelfLockCounts             Stats          `json:"selfLockCounts"`
	Ratings                    Ratings        `json:"ratings"`
	LockInfo                   LockInfo       `json:"lockInfo"`
	DeveloperCode              string         `json:"developercode"`
	ID                         ID             `json:"id"`
	Levels                     Levels         `json:"levels"`
	KeyholderLevels            map[string]any `json:"keyholderLevels"`
	Badges                     []any          `json:"badges"`
	Status                     Status         `json:"status"`
}

// String redacts the developer code.
func (p Profile) String() string {
	return fmt.Sprintf("chastisafe.Profile{user:%q, id:%q, developerCode:[redacted]}", p.User, p.ID)
}

// LogValue keeps the developer code out of structured logs.
func (p Profile) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("user", p.User),
		slog.String("id", string(p.ID)),
	)
}

// Stats is a free-form map because ChastiSafe adds purpose keys over time.
type Stats map[string]int

// Value returns a statistic, or zero when it is absent.
func (s Stats) Value(key string) int { return s[key] }

// Chastity is cumulative chastity time in seconds.
func (s Stats) Chastity() int { return s.Value("CHASTITY") }

// Ratings holds the account's rating summary.
type Ratings struct {
	RatingsAsLockeeCount     int     `json:"ratingsAsLockeeCount"`
	RatingsAsKeyholderCount  int     `json:"ratingsAsKeyholderCount"`
	AverageRatingAsLockee    float64 `json:"averageRatingAsLockee"`
	AverageRatingAsKeyholder float64 `json:"averageRatingAsKeyholder"`
}

// LockInfo is the small lock summary on a profile.
type LockInfo struct {
	ChastityLocks []LockInfoEntry `json:"chastityLocks"`
}

// LockInfoEntry is one currently loaded chastity lock on a profile.
type LockInfoEntry struct {
	Keyholder string `json:"keyholder"`
	LockName  string `json:"lockName"`
	LoadTime  Time   `json:"loadtime"`
}

// Levels holds the account's experience labels.
type Levels struct {
	ChastityLevel        string `json:"chastityLevel"`
	BondageLevel         string `json:"bondageLevel"`
	TaskLevel            string `json:"taskLevel"`
	RunningChastityLevel string `json:"runningChastityLevel"`
	RunningBondageLevel  string `json:"runningBondageLevel"`
	RunningTaskLevel     string `json:"runningTaskLevel"`
}

// TemplateCard is one card range in a card template.
type TemplateCard struct {
	Name Card `json:"name"`
	Min  int  `json:"min"`
	Max  int  `json:"max"`
}

// Template is a lock that a keyholder offers for loading.
type Template struct {
	ID                     ID             `json:"id"`
	Keyholder              string         `json:"keyholder"`
	KeyholderID            ID             `json:"keyholderId"`
	LockName               string         `json:"lockname"`
	LockType               string         `json:"locktype"`
	Hidden                 string         `json:"hidden"`
	StartFrozen            string         `json:"startfrozen"`
	CheckInPeriod          int            `json:"checkinperiod"`
	CheckInTolerance       int            `json:"checkintolerance"`
	FirstCheckIn           int            `json:"firstcheckin"`
	CheckInMissPenalty     FlexString     `json:"checkInMissPenalty"`
	TimerMin               int            `json:"timerMin"`
	TimerMax               int            `json:"timerMax"`
	Cumulative             string         `json:"cumulative"`
	Regularity             int            `json:"regularity"`
	AutoResetPeriod        int            `json:"autoresetperiod"`
	AutoResetCount         int            `json:"autoresetcount"`
	Cards                  []TemplateCard `json:"cards"`
	RequiredGreens         int            `json:"requiredgreens"`
	BotID                  ID             `json:"botid"`
	BotName                string         `json:"botname"`
	ContactKeyholder       bool           `json:"contactkeyholder"`
	BlockLocked            bool           `json:"blocklocked"`
	BlockTestLocks         bool           `json:"blocktestlocks"`
	FakesMin               int            `json:"fakesmin"`
	FakesMax               int            `json:"fakesmax"`
	TrustTime              int            `json:"trusttime"`
	DiceCount              int            `json:"dicecount"`
	DiceFaces              int            `json:"dicefaces"`
	Lockees                int            `json:"lockees"`
	HygieneUnlockPeriod    int            `json:"hygieneunlockperiod"`
	HygieneUnlockDuration  int            `json:"hygieneunlockduration"`
	FirstHygieneUnlock     int            `json:"firsthygieneunlock"`
	HygieneRelockPenalty   FlexString     `json:"hygieneRelockMissPenalty"`
	TaskRollerID           ID             `json:"taskrollerid"`
	TaskMissPenalty        FlexString     `json:"taskMissPenalty"`
	Status                 string         `json:"status"`
	AllowedPurposes        PurposeMask    `json:"allowedpurposes"`
	MinimumDuration        int            `json:"minimumduration"`
	MinimumDurationEnabled bool           `json:"minimumdurationenabled"`
	AllowUpdates           bool           `json:"allowupdates"`
	Description            string         `json:"description"`
	Passcode               string         `json:"passcode"`
	PinkScript             string         `json:"pinkscript"`
	Background             string         `json:"background"`
	IsKeyholder            bool           `json:"isKeyholder"`
	IsSelfLocked           bool           `json:"isSelfLocked"`
}

// IsCardLock reports whether the template creates a card lock.
func (t *Template) IsCardLock() bool {
	return strings.EqualFold(t.LockType, "cardlock") || strings.EqualFold(t.LockType, "card")
}

// IsTimerLock reports whether the template creates a timer lock.
func (t *Template) IsTimerLock() bool {
	return strings.EqualFold(t.LockType, "timedlock") || strings.EqualFold(t.LockType, "timer")
}

// AllowsPurpose reports whether the template accepts a lock purpose.
func (t *Template) AllowsPurpose(p Purpose) bool { return t.AllowedPurposes.Has(p) }

// LockButton is a script-backed button on a loaded lock.
type LockButton struct {
	ButtonText string `json:"buttonText"`
	ScriptID   ID     `json:"scriptId"`
}

// ScriptState is the current state of a lock script.
type ScriptState struct {
	Type       string `json:"type"`
	Code       string `json:"code"`
	Parameters []any  `json:"parameters"`
}

// LoadedLock is an active lock session.
type LoadedLock struct {
	ID                    ID           `json:"id"`
	LockName              string       `json:"lockname"`
	Keyholder             string       `json:"keyholder"`
	KeyholderID           ID           `json:"keyholderId"`
	KeyholderEmoji        string       `json:"keyholderEmoji,omitempty"`
	Lockee                string       `json:"lockee"`
	LockeeID              ID           `json:"lockeeId"`
	LockeeEmoji           string       `json:"lockeeEmoji,omitempty"`
	Hidden                bool         `json:"hidden"`
	LastResetTime         Time         `json:"lastresettime"`
	FrozenByKeyholder     bool         `json:"frozenByKeyholder"`
	FrozenByCard          bool         `json:"frozenByCard"`
	LoadTime              Time         `json:"loadtime"`
	LockeeIcon            int          `json:"lockeeIcon"`
	KeyholderIcon         int          `json:"keyholderIcon"`
	LockPurpose           Purpose      `json:"lockpurpose"`
	Flags                 int          `json:"flags"`
	Buttons               []LockButton `json:"buttons"`
	Type                  string       `json:"type"`
	Cumulative            bool         `json:"cumulative"`
	MaxPicks              int          `json:"maxPicks"`
	Deck                  Deck         `json:"deck,omitempty"`
	GreensRequired        int          `json:"greensrequired"`
	Regularity            int          `json:"regularity"`
	Picks                 int          `json:"picks"`
	Draws                 Draws        `json:"draws,omitempty"`
	Wait                  int          `json:"wait"`
	FreezeDuration        int          `json:"freezeDuration"`
	DiceCount             int          `json:"dicecount"`
	DiceFaces             int          `json:"dicefaces"`
	ReadyToUnlock         bool         `json:"readyToUnlock"`
	AutoResetsRemaining   int          `json:"autoresetsremaining"`
	AutoResetPeriod       int          `json:"autoresetperiod"`
	AutoResetCount        int          `json:"autoresetcount"`
	CardCount             int          `json:"cardcount"`
	TimerRemaining        int          `json:"timeremaining"`
	CheckInPeriod         int          `json:"checkinperiod"`
	CheckInTolerance      int          `json:"checkintolerance"`
	NextCheckInTime       Time         `json:"nextcheckintime"`
	TimeToNextCheckIn     int          `json:"timetonextcheckin"`
	CheckInMissPenalty    FlexString   `json:"checkInMissPenalty"`
	TimeToHygieneUnlock   int          `json:"timetohygieneunlock"`
	HygieneUnlockStatus   FlexString   `json:"hygieneunlockstatus"`
	HygieneRelockDigits   int          `json:"hygienerelockdigits"`
	HygieneRelockCharset  string       `json:"hygienerelockcharset"`
	HygieneUnlockPeriod   int          `json:"hygieneunlockperiod"`
	HygieneUnlockDuration int          `json:"hygieneunlockduration"`
	HygieneRelockPenalty  FlexString   `json:"hygieneRelockMissPenalty"`
	RealLockID            ID           `json:"reallockid"`
	TrustEndTime          Time         `json:"trustendtime"`
	NextAutoReset         Time         `json:"nextAutoReset"`
	RemainingGuaranteed   int          `json:"remainingguaranteed"`
	Combination           string       `json:"combination,omitempty"`
	TaskRollerID          ID           `json:"taskRollerId"`
	MissedTaskPenalty     FlexString   `json:"missedTaskPenalty"`
	NextTaskPick          int          `json:"nextTaskPick"`
	NextTaskComplete      int          `json:"nextTaskComplete"`
	TaskMinRegularity     int          `json:"taskMinRegularity"`
	BotID                 ID           `json:"botId"`
	BotName               string       `json:"botName"`
	PilloryRemaining      int          `json:"pilloryRemaining"`
	PilloryMinVotes       int          `json:"pilloryMinVotes"`
	PilloryTask           *TaskItem    `json:"pilloryTask"`
	ScriptState           *ScriptState `json:"scriptState"`
	Template              *Template    `json:"template,omitempty"`
}

// String redacts the combination.
func (l LoadedLock) String() string {
	return fmt.Sprintf("chastisafe.LoadedLock{id:%q, name:%q, type:%q, status:%q}", l.ID, l.LockName, l.Type, l.Status())
}

// LogValue keeps the combination out of structured logs.
func (l LoadedLock) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("id", string(l.ID)),
		slog.String("name", l.LockName),
		slog.String("type", l.Type),
		slog.String("status", l.Status()),
	)
}

// IsCardLock reports whether this is a card lock.
func (l *LoadedLock) IsCardLock() bool {
	return strings.EqualFold(l.Type, "card") || strings.EqualFold(l.Type, "cardlock")
}

// IsTimerLock reports whether this is a timer lock.
func (l *LoadedLock) IsTimerLock() bool {
	return strings.EqualFold(l.Type, "timer") || strings.EqualFold(l.Type, "timedlock")
}

// IsSelfLock reports whether the account is holding its own key.
func (l *LoadedLock) IsSelfLock() bool {
	return strings.EqualFold(l.Keyholder, "<self>") || l.KeyholderID == "0" || l.KeyholderID == ""
}

// IsFrozen reports whether the lock is frozen by either source.
func (l *LoadedLock) IsFrozen() bool { return l.FrozenByKeyholder || l.FrozenByCard }

// Remaining is the timer remaining on a timer lock.
func (l *LoadedLock) Remaining() time.Duration {
	return time.Duration(l.TimerRemaining) * time.Second
}

// NextPickIn is the wait before the next card can be picked.
func (l *LoadedLock) NextPickIn() time.Duration {
	return time.Duration(l.Wait) * time.Second
}

// NextCheckInIn is the wait before the next required check-in.
func (l *LoadedLock) NextCheckInIn() time.Duration {
	if l.TimeToNextCheckIn > 0 {
		return time.Duration(l.TimeToNextCheckIn) * time.Second
	}
	if !l.NextCheckInTime.IsZero() {
		if remaining := time.Until(l.NextCheckInTime.Time); remaining > 0 {
			return remaining
		}
	}
	return 0
}

// NextHygieneIn is the wait before the next hygiene unlock.
func (l *LoadedLock) NextHygieneIn() time.Duration {
	return time.Duration(l.TimeToHygieneUnlock) * time.Second
}

// GreensRemaining is the number of greens still required. The app stores
// 1000 or less for a fixed count and anything above 1000 for "all greens".
func (l *LoadedLock) GreensRemaining() int {
	if l.GreensRequired > 1000 {
		return l.Deck.Greens()
	}
	remaining := l.GreensRequired - l.Draws.Count(CardGreen)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CardsRemaining is the number of cards left.
func (l *LoadedLock) CardsRemaining() int {
	if l.CardCount > 0 {
		return l.CardCount
	}
	return l.Deck.Total()
}

// IsReady reports whether the lock can be opened.
func (l *LoadedLock) IsReady() bool { return l.ReadyToUnlock }

// Status is a short human-readable state.
func (l *LoadedLock) Status() string {
	switch {
	case l.IsFrozen():
		return "frozen"
	case l.IsReady():
		return "ready to unlock"
	case l.IsCardLock():
		return "locked"
	case l.IsTimerLock():
		return "locked"
	default:
		return "locked"
	}
}

// RecentLock is one completed lock from /recent.
type RecentLock struct {
	LockID       ID      `json:"lockid"`
	TemplateID   ID      `json:"templateid"`
	LockName     string  `json:"lockname"`
	Lockee       string  `json:"lockee"`
	Keyholder    string  `json:"keyholder"`
	UnlockReason int     `json:"unlockreason"`
	Rating       int     `json:"rating"`
	LoadTime     Time    `json:"loadtime"`
	UnlockTime   Time    `json:"unlocktime"`
	LockDuration int     `json:"lockduration"`
	LockCode     string  `json:"lockcode,omitempty"`
	Fake         bool    `json:"fake,omitempty"`
	LockPurpose  Purpose `json:"lockpurpose"`
}

// Unlock reason values used by RecentLock.
const (
	UnlockReasonCompleted = 0
	UnlockReasonKeyholder = 1
	UnlockReasonAbandoned = 2
)

// UnlockReasonString describes a history unlock reason.
func UnlockReasonString(reason int) string {
	switch reason {
	case UnlockReasonCompleted:
		return "completed"
	case UnlockReasonKeyholder:
		return "unlocked by keyholder"
	case UnlockReasonAbandoned:
		return "abandoned"
	default:
		return "unknown"
	}
}

// LogEntry is one lock history event.
type LogEntry struct {
	Timestamp Time   `json:"timestamp"`
	Actor     int    `json:"actor"`
	EventType int    `json:"event_type"`
	Amount    int    `json:"amount"`
	Hidden    int    `json:"hidden"`
	Card      string `json:"card,omitempty"`
}

// TaskItem is one task in a task roller.
type TaskItem struct {
	ID          ID         `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ItemType    string     `json:"itemType"`
	Param1      FlexString `json:"param1"`
	Param2      FlexString `json:"param2"`
	Points      int        `json:"points"`
}

// Task is a task roller.
type Task struct {
	ID               ID         `json:"id"`
	LockID           ID         `json:"lockId"`
	TaskType         string     `json:"taskType"`
	TaskDecider      string     `json:"taskDecider"`
	TaskVisibility   string     `json:"taskVisibility"`
	TaskCompletion   string     `json:"taskCompletion"`
	PilloryType      string     `json:"pilloryType"`
	Name             string     `json:"name"`
	EndRule          string     `json:"endRule"`
	MinRegularity    int        `json:"minRegularity"`
	Tolerance        int        `json:"tolerance"`
	Items            []TaskItem `json:"items"`
	CompletionTarget FlexString `json:"completionTarget"`
	PointsNeeded     int        `json:"pointsNeeded"`
}

// TaskStatus is the per-lock task state.
type TaskStatus struct {
	PointsNeeded int              `json:"pointsNeeded"`
	Tasks        []TaskStatusItem `json:"tasks"`
}

// TaskStatusItem is one assignable or active task.
type TaskStatusItem struct {
	Active     bool     `json:"active"`
	Completed  bool     `json:"completed"`
	Assignable bool     `json:"assignable"`
	Item       TaskItem `json:"item"`
}

// TaskPick is the result of drawing a task.
type TaskPick struct {
	Status   Status    `json:"status"`
	Done     bool      `json:"done"`
	Phase    string    `json:"phase"`
	TaskItem *TaskItem `json:"taskItem"`
}

// Pillory is a vote-based punishment session.
type Pillory struct {
	Items            []PilloryItem `json:"items"`
	Lockee           string        `json:"lockee"`
	Keyholder        string        `json:"keyholder"`
	LockID           ID            `json:"lockId"`
	EndRule          string        `json:"endRule"`
	MinVotes         int           `json:"minVotes"`
	PilloryRemaining int           `json:"pilloryRemaining"`
	TaskRollerName   string        `json:"taskRollerName"`
	LockType         string        `json:"lockType"`
}

// PilloryItem is one vote option.
type PilloryItem struct {
	Active  bool     `json:"active"`
	Item    TaskItem `json:"item"`
	Votes   int      `json:"votes"`
	OwnVote int      `json:"ownVote"`
}

// Script is a saved lock script.
type Script struct {
	TemplateID ID     `json:"templateId"`
	ScriptID   ID     `json:"scriptId"`
	Name       string `json:"name"`
	RunAtLoad  bool   `json:"runAtLoad"`
	ScriptText string `json:"scriptText"`
}

// Bot is a ChastiSafe keyholder bot.
type Bot struct {
	ID       ID     `json:"botid"`
	Name     string `json:"botname"`
	LockMask int    `json:"botlockmask"`
}

// LoadRequest starts a lock. Fakes and Passcode are always sent, matching the
// web client.
type LoadRequest struct {
	TemplateID ID       `json:"templateid"`
	Code       string   `json:"code"`
	Purpose    Purpose  `json:"lockpurpose,omitempty"`
	Fakes      []string `json:"fakes"`
	Trust      bool     `json:"trust"`
	Passcode   *string  `json:"passcode"`
}

// OpenResult is the response from open, unlock, reset and similar actions.
type OpenResult struct {
	Status Status      `json:"status"`
	Locked bool        `json:"locked"`
	Code   string      `json:"code,omitempty"`
	Reason string      `json:"reason,omitempty"`
	Lock   *LoadedLock `json:"lock,omitempty"`
}

// PickResult is the response from drawing a card.
type PickResult struct {
	Status Status      `json:"status"`
	Reason string      `json:"reason,omitempty"`
	Card   Card        `json:"card,omitempty"`
	Dice   int         `json:"dice,omitempty"`
	Done   bool        `json:"done,omitempty"`
	Wait   int         `json:"wait,omitempty"`
	Lock   *LoadedLock `json:"lock,omitempty"`
}

// ActionStatus is the response from actions that only report success.
type ActionStatus struct {
	Status Status `json:"status"`
	Locked bool   `json:"locked"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// CardChange is one card delta for UpdateCards.
type CardChange struct {
	Card  Card `json:"card"`
	Delta int  `json:"delta"`
}

// UpdateOptions carries the optional message attached to a card update.
type UpdateOptions struct {
	Message string
	Show    bool
}

// KeyholderSettings is a partial update of a loaded lock's settings. Only
// non-nil fields are sent. Durations are in the units ChastiSafe uses:
// Regularity and CheckInPeriod are minutes, Remaining and AddTime are seconds.
type KeyholderSettings struct {
	FrozenByKeyholder     *bool
	Hidden                *bool
	Cumulative            *bool
	MaxPicks              *int
	CurrentPicks          *int
	Regularity            *int
	CheckInPeriod         *int
	CheckInTolerance      *int
	TimeToNextCheckIn     *int
	CheckInMissPenalty    *string
	HygieneUnlockPeriod   *int
	HygieneUnlockDuration *int
	TimeToHygieneUnlock   *int
	HygieneRelockPenalty  *string
	AutoResetPeriod       *int
	AutoResetCount        *int
	BotID                 *string
	TaskID                *string
	MissedTaskPenalty     *string
	RemainingTime         *int
	AddTime               *int
	LockPurpose           *Purpose
}

func (s KeyholderSettings) payload(lockID ID) map[string]any {
	body := map[string]any{"id": lockID}
	if s.FrozenByKeyholder != nil {
		body["frozenbykh"] = *s.FrozenByKeyholder
	}
	if s.Hidden != nil {
		body["hidden"] = *s.Hidden
	}
	if s.Cumulative != nil {
		body["cumulative"] = *s.Cumulative
	}
	if s.MaxPicks != nil {
		body["maxPicks"] = strconv.Itoa(*s.MaxPicks)
	}
	if s.CurrentPicks != nil {
		body["curPicks"] = strconv.Itoa(*s.CurrentPicks)
	}
	if s.Regularity != nil {
		body["regularity"] = strconv.Itoa(*s.Regularity)
	}
	if s.CheckInPeriod != nil {
		body["checkinperiod"] = strconv.Itoa(*s.CheckInPeriod)
	}
	if s.CheckInTolerance != nil {
		body["checkintolerance"] = strconv.Itoa(*s.CheckInTolerance)
	}
	if s.TimeToNextCheckIn != nil {
		body["timetonextcheckin"] = strconv.Itoa(*s.TimeToNextCheckIn)
	}
	if s.CheckInMissPenalty != nil {
		body["checkInMissPenalty"] = *s.CheckInMissPenalty
	}
	if s.HygieneUnlockPeriod != nil {
		body["hygieneunlockperiod"] = strconv.Itoa(*s.HygieneUnlockPeriod)
	}
	if s.HygieneUnlockDuration != nil {
		body["hygieneunlockduration"] = strconv.Itoa(*s.HygieneUnlockDuration)
	}
	if s.TimeToHygieneUnlock != nil {
		body["timetohygieneunlock"] = strconv.Itoa(*s.TimeToHygieneUnlock)
	}
	if s.HygieneRelockPenalty != nil {
		body["hygieneRelockMissPenalty"] = *s.HygieneRelockPenalty
	}
	if s.AutoResetPeriod != nil {
		body["autoresetperiod"] = strconv.Itoa(*s.AutoResetPeriod)
	}
	if s.AutoResetCount != nil {
		body["autoresetcount"] = strconv.Itoa(*s.AutoResetCount)
	}
	if s.BotID != nil {
		body["botid"] = *s.BotID
	}
	if s.TaskID != nil {
		body["taskid"] = *s.TaskID
	}
	if s.MissedTaskPenalty != nil {
		body["missedTaskPenalty"] = *s.MissedTaskPenalty
	}
	if s.RemainingTime != nil {
		body["remainingtime"] = *s.RemainingTime
	}
	if s.AddTime != nil {
		body["addtime"] = *s.AddTime
	}
	if s.LockPurpose != nil {
		body["lockPurpose"] = string(*s.LockPurpose)
	}
	return body
}
