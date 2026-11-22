package chastisafe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TemplateType is the two template shapes the bot can create.
type TemplateType string

const (
	TemplateTypeTimer TemplateType = "timer"
	TemplateTypeCard  TemplateType = "card"
)

// ErrTemplateNotListed means a create succeeded but the new template did not
// appear in the account's list yet.
var ErrTemplateNotListed = errors.New("chastisafe: template was created but is not listed yet")

// TemplateDraft is a typed create request. Payload fills the many fields the
// ChastiSafe web client always sends.
type TemplateDraft struct {
	Name            string
	Type            TemplateType
	Description     string
	TimerMin        time.Duration
	TimerMax        time.Duration
	Cards           []TemplateCard
	Regularity      time.Duration
	AllowedPurposes PurposeMask
	Hidden          bool
	StartFrozen     bool
	AllowUpdates    *bool
	Cumulative      *bool
	RequiredGreens  *int
	MinimumDuration time.Duration
}

const defaultTemplateBackground = "pexels-pixabay-65650.jpg"

// Payload validates the draft and builds a create payload.
func (d TemplateDraft) Payload() (map[string]any, error) {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return nil, errors.New("chastisafe: template name is empty")
	}
	if d.Type != TemplateTypeTimer && d.Type != TemplateTypeCard {
		return nil, errors.New("chastisafe: template type must be timer or card")
	}
	if d.TimerMin < 0 || d.TimerMax < 0 {
		return nil, errors.New("chastisafe: timer bounds cannot be negative")
	}
	if d.TimerMax > 0 && d.TimerMin > d.TimerMax {
		return nil, errors.New("chastisafe: timer minimum is larger than timer maximum")
	}
	regularity, err := wholeMinutes(d.Regularity)
	if err != nil {
		return nil, fmt.Errorf("chastisafe: regularity: %w", err)
	}
	minimum, err := wholeSeconds(d.MinimumDuration)
	if err != nil {
		return nil, fmt.Errorf("chastisafe: minimum duration: %w", err)
	}

	allowed := d.AllowedPurposes
	if allowed == 0 {
		allowed = PurposeMaskTest | PurposeMaskChastity
	}

	lockType := "TIMEDLOCK"
	cumulative := "no"
	cards := []map[string]any{}
	requiredGreens := 0
	if d.Type == TemplateTypeCard {
		lockType = "CARDLOCK"
		cumulative = "yes"
		requiredGreens = 1001
		if len(d.Cards) == 0 {
			return nil, errors.New("chastisafe: a card template needs at least one card range")
		}
		for _, card := range d.Cards {
			if !card.Name.Valid() {
				return nil, fmt.Errorf("chastisafe: unknown card %q", card.Name)
			}
			if card.Min < 0 || card.Max < 0 || card.Min > card.Max {
				return nil, fmt.Errorf("chastisafe: card %s has an invalid range", card.Name)
			}
			cards = append(cards, map[string]any{
				"name": string(card.Name),
				"min":  card.Min,
				"max":  card.Max,
			})
		}
	} else if len(d.Cards) > 0 {
		return nil, errors.New("chastisafe: a timer template cannot have cards")
	}

	allowUpdates := true
	if d.AllowUpdates != nil {
		allowUpdates = *d.AllowUpdates
	}
	if d.Cumulative != nil {
		if *d.Cumulative {
			cumulative = "yes"
		} else {
			cumulative = "no"
		}
	}
	if d.RequiredGreens != nil && d.Type == TemplateTypeCard {
		requiredGreens = *d.RequiredGreens
	}

	return map[string]any{
		"id":                       "id",
		"keyholder":                nil,
		"lockname":                 name,
		"locktype":                 lockType,
		"hidden":                   yesNo(d.Hidden),
		"startfrozen":              yesNo(d.StartFrozen),
		"checkinperiod":            0,
		"checkintolerance":         0,
		"firstcheckin":             0,
		"checkInMissPenalty":       "",
		"timerMin":                 int(d.TimerMin / time.Second),
		"timerMax":                 int(d.TimerMax / time.Second),
		"cumulative":               cumulative,
		"regularity":               regularity,
		"autoresetperiod":          0,
		"autoresetcount":           0,
		"cards":                    cards,
		"requiredgreens":           requiredGreens,
		"botid":                    nil,
		"botname":                  nil,
		"contactkeyholder":         false,
		"blocklocked":              false,
		"blocktestlocks":           false,
		"fakesmin":                 0,
		"fakesmax":                 0,
		"trusttime":                0,
		"dicecount":                1,
		"dicefaces":                6,
		"lockees":                  0,
		"hygieneunlockperiod":      0,
		"hygieneunlockduration":    0,
		"firsthygieneunlock":       0,
		"hygieneRelockMissPenalty": "",
		"taskrollerid":             nil,
		"taskMissPenalty":          "",
		"status":                   "ENABLED",
		"allowedpurposes":          int(allowed),
		"minimumduration":          minimum,
		"minimumdurationenabled":   d.MinimumDuration > 0,
		"allowupdates":             allowUpdates,
		"description":              strings.TrimSpace(d.Description),
		"passcode":                 nil,
		"pinkscript":               "",
		"background":               defaultTemplateBackground,
	}, nil
}

// TemplatePatch is a partial update. Only non-nil fields are sent.
type TemplatePatch struct {
	Name                   *string
	Description            *string
	Hidden                 *bool
	StartFrozen            *bool
	AllowUpdates           *bool
	Cumulative             *bool
	TimerMin               *time.Duration
	TimerMax               *time.Duration
	Regularity             *time.Duration
	CheckInPeriod          *time.Duration
	CheckInTolerance       *time.Duration
	FirstCheckIn           *time.Duration
	HygieneUnlockPeriod    *time.Duration
	HygieneUnlockDuration  *time.Duration
	FirstHygieneUnlock     *time.Duration
	AutoResetPeriod        *time.Duration
	AutoResetCount         *int
	MinimumDuration        *time.Duration
	MinimumDurationEnabled *bool
	AllowedPurposes        *PurposeMask
	Status                 *string
}

// Payload validates the patch and builds an updatetemplate payload.
func (p TemplatePatch) Payload(id ID) (map[string]any, error) {
	if id == "" {
		return nil, errors.New("chastisafe: template id is empty")
	}
	payload := map[string]any{"templateId": id}
	empty := true

	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if name == "" {
			return nil, errors.New("chastisafe: template name is empty")
		}
		payload["lockname"] = name
		empty = false
	}
	if p.Description != nil {
		payload["description"] = strings.TrimSpace(*p.Description)
		empty = false
	}
	if p.Hidden != nil {
		payload["hidden"] = yesNo(*p.Hidden)
		empty = false
	}
	if p.StartFrozen != nil {
		payload["startfrozen"] = yesNo(*p.StartFrozen)
		empty = false
	}
	if p.AllowUpdates != nil {
		payload["allowupdates"] = *p.AllowUpdates
		empty = false
	}
	if p.Cumulative != nil {
		payload["cumulative"] = yesNo(*p.Cumulative)
		empty = false
	}
	if p.TimerMin != nil {
		seconds, err := wholeSeconds(*p.TimerMin)
		if err != nil {
			return nil, fmt.Errorf("chastisafe: timer minimum: %w", err)
		}
		payload["timerMin"] = seconds
		empty = false
	}
	if p.TimerMax != nil {
		seconds, err := wholeSeconds(*p.TimerMax)
		if err != nil {
			return nil, fmt.Errorf("chastisafe: timer maximum: %w", err)
		}
		payload["timerMax"] = seconds
		empty = false
	}
	for key, value := range map[string]*time.Duration{
		"regularity":            p.Regularity,
		"checkinperiod":         p.CheckInPeriod,
		"checkintolerance":      p.CheckInTolerance,
		"firstcheckin":          p.FirstCheckIn,
		"hygieneunlockperiod":   p.HygieneUnlockPeriod,
		"hygieneunlockduration": p.HygieneUnlockDuration,
		"firsthygieneunlock":    p.FirstHygieneUnlock,
		"autoresetperiod":       p.AutoResetPeriod,
	} {
		if value == nil {
			continue
		}
		minutes, err := wholeMinutes(*value)
		if err != nil {
			return nil, fmt.Errorf("chastisafe: %s: %w", key, err)
		}
		payload[key] = minutes
		empty = false
	}
	if p.AutoResetCount != nil {
		payload["autoresetcount"] = *p.AutoResetCount
		empty = false
	}
	if p.MinimumDuration != nil {
		seconds, err := wholeSeconds(*p.MinimumDuration)
		if err != nil {
			return nil, fmt.Errorf("chastisafe: minimum duration: %w", err)
		}
		payload["minimumduration"] = seconds
		if p.MinimumDurationEnabled == nil {
			payload["minimumdurationenabled"] = *p.MinimumDuration > 0
		}
		empty = false
	}
	if p.MinimumDurationEnabled != nil {
		payload["minimumdurationenabled"] = *p.MinimumDurationEnabled
		empty = false
	}
	if p.AllowedPurposes != nil {
		payload["allowedpurposes"] = int(*p.AllowedPurposes)
		empty = false
	}
	if p.Status != nil {
		status := strings.ToUpper(strings.TrimSpace(*p.Status))
		if status != "ENABLED" && status != "DISABLED" {
			return nil, errors.New("chastisafe: status must be ENABLED or DISABLED")
		}
		payload["status"] = status
		empty = false
	}
	if empty {
		return nil, errors.New("chastisafe: no template fields were supplied")
	}
	return payload, nil
}

// CreateTemplate creates a template and returns the new row when ChastiSafe
// lists it. A create response does not carry an ID.
func (s *Service) CreateTemplate(ctx context.Context, draft TemplateDraft) (*Template, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chastisafe: service has no client")
	}
	payload, err := draft.Payload()
	if err != nil {
		return nil, err
	}

	s.invalidateTemplates()
	before, _ := s.Templates(ctx)
	oldIDs := make(map[string]struct{}, len(before))
	for _, template := range before {
		oldIDs[template.ID.String()] = struct{}{}
	}

	if err := s.client.CreateTemplate(ctx, payload); err != nil {
		return nil, err
	}
	s.invalidateTemplates()

	for attempt := 0; attempt < 3; attempt++ {
		templates, err := s.Templates(ctx)
		if err == nil {
			matches := newTemplates(templates, oldIDs, draft.Name)
			switch len(matches) {
			case 1:
				copy := matches[0]
				return &copy, nil
			case 0:
				// The list may lag behind the write. Try again shortly.
			default:
				return nil, &AmbiguousRefError{Ref: draft.Name, Candidates: templateCandidates(matches)}
			}
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}

	return &Template{
		LockName:    strings.TrimSpace(draft.Name),
		LockType:    string(draft.Type),
		Description: strings.TrimSpace(draft.Description),
	}, ErrTemplateNotListed
}

func newTemplates(templates []Template, oldIDs map[string]struct{}, name string) []Template {
	want := strings.ToLower(strings.TrimSpace(name))
	matches := make([]Template, 0, 1)
	for _, template := range templates {
		if _, exists := oldIDs[template.ID.String()]; exists {
			continue
		}
		if strings.ToLower(strings.TrimSpace(template.LockName)) == want {
			matches = append(matches, template)
		}
	}
	return matches
}

// CloneTemplate copies one template into a new name.
func (s *Service) CloneTemplate(ctx context.Context, ref, name string) (*Template, error) {
	source, err := s.ResolveTemplate(ctx, ref)
	if err != nil {
		return nil, err
	}
	full := source
	if s.client != nil {
		if edited, editErr := s.client.TemplateForEdit(ctx, source.ID); editErr == nil && edited != nil {
			full = edited
		}
	}
	draft := draftFromTemplate(full, name)
	return s.CreateTemplate(ctx, draft)
}

func draftFromTemplate(template *Template, name string) TemplateDraft {
	draft := TemplateDraft{
		Name:            strings.TrimSpace(name),
		Description:     template.Description,
		AllowedPurposes: template.AllowedPurposes,
		Hidden:          strings.EqualFold(template.Hidden, "yes"),
		StartFrozen:     strings.EqualFold(template.StartFrozen, "yes"),
		TimerMin:        time.Duration(template.TimerMin) * time.Second,
		TimerMax:        time.Duration(template.TimerMax) * time.Second,
		Regularity:      time.Duration(template.Regularity) * time.Minute,
	}
	if template.IsCardLock() {
		draft.Type = TemplateTypeCard
		draft.Cards = append([]TemplateCard(nil), template.Cards...)
	} else {
		draft.Type = TemplateTypeTimer
	}
	if template.Cumulative != "" {
		value := strings.EqualFold(template.Cumulative, "yes")
		draft.Cumulative = &value
	}
	if template.AllowUpdates {
		value := true
		draft.AllowUpdates = &value
	} else {
		value := false
		draft.AllowUpdates = &value
	}
	return draft
}

// UpdateTemplate applies a partial update and returns the refreshed template.
func (s *Service) UpdateTemplate(ctx context.Context, ref string, patch TemplatePatch) (*Template, error) {
	resolved, err := s.ResolveTemplate(ctx, ref)
	if err != nil {
		return nil, err
	}
	payload, err := patch.Payload(resolved.ID)
	if err != nil {
		return nil, err
	}
	if err := s.client.UpdateTemplate(ctx, payload); err != nil {
		return nil, err
	}
	s.invalidateTemplates()
	s.invalidateLocks()

	updated, err := s.client.TemplateForEdit(ctx, resolved.ID)
	if err != nil || updated == nil {
		return resolved, nil
	}
	return updated, nil
}

// DeleteTemplate removes a template after resolving its reference.
func (s *Service) DeleteTemplate(ctx context.Context, ref string) error {
	resolved, err := s.ResolveTemplate(ctx, ref)
	if err != nil {
		return err
	}
	if err := s.client.DeleteTemplate(ctx, resolved.ID); err != nil {
		return err
	}
	s.invalidateTemplates()
	return nil
}

// SimulateTemplate previews a card template's duration and card counts.
func (s *Service) SimulateTemplate(ctx context.Context, ref string) (*Simulation, error) {
	resolved, err := s.ResolveTemplate(ctx, ref)
	if err != nil {
		return nil, err
	}
	return s.client.Simulate(ctx, resolved.ID)
}

// UpdateTemplateCard changes one card range on a template.
func (s *Service) UpdateTemplateCard(ctx context.Context, ref string, change CardRange) error {
	resolved, err := s.ResolveTemplate(ctx, ref)
	if err != nil {
		return err
	}
	if err := s.client.UpdateCardRange(ctx, resolved.ID, change); err != nil {
		return err
	}
	s.invalidateTemplates()
	return nil
}

// SetTemplateBackground changes a template's background image.
func (s *Service) SetTemplateBackground(ctx context.Context, ref, background string) error {
	resolved, err := s.ResolveTemplate(ctx, ref)
	if err != nil {
		return err
	}
	if err := s.client.SetBackground(ctx, resolved.ID, strings.TrimSpace(background)); err != nil {
		return err
	}
	s.invalidateTemplates()
	return nil
}

// TemplateForEdit returns one template including private edit fields.
func (s *Service) TemplateForEdit(ctx context.Context, ref string) (*Template, error) {
	resolved, err := s.ResolveTemplate(ctx, ref)
	if err != nil {
		return nil, err
	}
	return s.client.TemplateForEdit(ctx, resolved.ID)
}

func wholeMinutes(duration time.Duration) (int, error) {
	if duration == 0 {
		return 0, nil
	}
	if duration%time.Minute != 0 {
		return 0, errors.New("must be a whole number of minutes")
	}
	return int(duration / time.Minute), nil
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
