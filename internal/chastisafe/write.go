package chastisafe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Load starts a lock from a template.
func (c *Client) Load(ctx context.Context, req LoadRequest) ([]LoadedLock, error) {
	const op = "POST /load"
	if req.TemplateID == "" {
		return nil, errors.New("chastisafe: load needs a template id")
	}
	if req.Fakes == nil {
		req.Fakes = []string{}
	}
	body, err := c.post(ctx, "/load", req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Locks  []LoadedLock `json:"locks"`
		Status Status       `json:"status"`
		Reason string       `json:"reason"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if len(out.Locks) > 0 {
		return out.Locks, nil
	}
	return nil, newAPIError(op, 200, nil, body)
}

// Open opens a lock for its lockee. It only succeeds when the lock is ready;
// otherwise OpenResult and ErrLockNotReady are returned.
func (c *Client) Open(ctx context.Context, lockID ID) (*OpenResult, error) {
	return c.openLike(ctx, "GET /open/{id}", "/open/"+url.PathEscape(lockID.String()))
}

// Unlock opens a lockee's lock as its keyholder. ChastiSafe returns
// locknotfound when the caller is a self-lock and therefore has no keyholder.
func (c *Client) Unlock(ctx context.Context, lockID ID) (*OpenResult, error) {
	return c.openLike(ctx, "GET /unlock/{id}", "/unlock/"+url.PathEscape(lockID.String()))
}

// Reset restarts a lock from the beginning.
func (c *Client) Reset(ctx context.Context, lockID ID) (*OpenResult, error) {
	return c.openLike(ctx, "GET /reset/{id}", "/reset/"+url.PathEscape(lockID.String()))
}

// RandomUnlock randomly unlocks a lock. ChastiSafe calls this "random".
func (c *Client) RandomUnlock(ctx context.Context, lockID ID) (*OpenResult, error) {
	return c.openLike(ctx, "GET /random/{id}", "/random/"+url.PathEscape(lockID.String()))
}

func (c *Client) openLike(ctx context.Context, op, path string) (*OpenResult, error) {
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	var out OpenResult
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		if out.Reason != "" {
			return &out, newAPIError(op, 200, nil, body)
		}
		if out.Locked {
			return &out, ErrLockNotReady
		}
		return &out, newAPIError(op, 200, nil, body)
	}
	return &out, nil
}

// Abandon ends a lock without revealing its combination.
func (c *Client) Abandon(ctx context.Context, lockID ID) error {
	const op = "GET /abandon/{id}"
	body, err := c.get(ctx, "/abandon/"+url.PathEscape(lockID.String()))
	if err != nil {
		return err
	}
	var out ActionStatus
	if err := decodeJSON(body, &out); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// Pick draws the next card from a card lock.
func (c *Client) Pick(ctx context.Context, lockID ID) (*PickResult, error) {
	const op = "GET /pick/{id}"
	body, err := c.get(ctx, "/pick/"+url.PathEscape(lockID.String()))
	if err != nil {
		return nil, err
	}
	var out PickResult
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		return &out, newAPIError(op, 200, nil, body)
	}
	return &out, nil
}

// AddTime adds or removes timer seconds. Negative values remove time.
func (c *Client) AddTime(ctx context.Context, lockID ID, delta time.Duration) (*LoadedLock, error) {
	seconds, err := wholeSeconds(delta)
	if err != nil {
		return nil, err
	}
	return c.update(ctx, "POST /update addtime", map[string]any{
		"id":      lockID,
		"addtime": seconds,
	})
}

// SetRemainingTime replaces a timer's remaining time. This is a keyholder
// action.
func (c *Client) SetRemainingTime(ctx context.Context, lockID ID, remaining time.Duration) (*LoadedLock, error) {
	seconds, err := wholeSeconds(remaining)
	if err != nil {
		return nil, err
	}
	return c.update(ctx, "POST /update remainingtime", map[string]any{
		"id":            lockID,
		"remainingtime": seconds,
	})
}

// SetPurpose changes a lock's purpose.
func (c *Client) SetPurpose(ctx context.Context, lockID ID, purpose Purpose) (*LoadedLock, error) {
	if !purpose.Valid() {
		return nil, fmt.Errorf("chastisafe: unknown purpose %q", purpose)
	}
	return c.update(ctx, "POST /update lockPurpose", map[string]any{
		"id":          lockID,
		"lockPurpose": string(purpose),
	})
}

// Freeze freezes or unfreezes a lock as its keyholder.
func (c *Client) Freeze(ctx context.Context, lockID ID, frozen bool) (*LoadedLock, error) {
	return c.update(ctx, "POST /update frozenbykh", map[string]any{
		"id":         lockID,
		"frozenbykh": frozen,
	})
}

// Hide hides or reveals a lock's timer or cards as its keyholder.
func (c *Client) Hide(ctx context.Context, lockID ID, hidden bool) (*LoadedLock, error) {
	return c.update(ctx, "POST /update hidden", map[string]any{
		"id":     lockID,
		"hidden": hidden,
	})
}

// UpdateSettings sends a partial keyholder settings update.
func (c *Client) UpdateSettings(ctx context.Context, lockID ID, settings KeyholderSettings) (*LoadedLock, error) {
	return c.update(ctx, "POST /update settings", settings.payload(lockID))
}

// UpdateCards adds or removes cards and attaches an optional update message.
func (c *Client) UpdateCards(ctx context.Context, lockID ID, changes []CardChange, opts UpdateOptions) (*LoadedLock, error) {
	if len(changes) == 0 {
		return nil, errors.New("chastisafe: no card changes")
	}
	for _, change := range changes {
		if !change.Card.Valid() {
			return nil, fmt.Errorf("chastisafe: unknown card %q", change.Card)
		}
		if change.Delta == 0 {
			return nil, fmt.Errorf("chastisafe: card %s has a zero delta", change.Card)
		}
	}
	return c.update(ctx, "POST /update cards", map[string]any{
		"id":         lockID,
		"cards":      changes,
		"message":    opts.Message,
		"showupdate": opts.Show,
	})
}

// AddCards adds cards to a card lock.
func (c *Client) AddCards(ctx context.Context, lockID ID, cards map[Card]int, opts UpdateOptions) (*LoadedLock, error) {
	changes := make([]CardChange, 0, len(cards))
	for card, count := range cards {
		if count <= 0 {
			continue
		}
		changes = append(changes, CardChange{Card: card, Delta: count})
	}
	return c.UpdateCards(ctx, lockID, changes, opts)
}

// RemoveCards removes cards from a card lock.
func (c *Client) RemoveCards(ctx context.Context, lockID ID, cards map[Card]int, opts UpdateOptions) (*LoadedLock, error) {
	changes := make([]CardChange, 0, len(cards))
	for card, count := range cards {
		if count <= 0 {
			continue
		}
		changes = append(changes, CardChange{Card: card, Delta: -count})
	}
	return c.UpdateCards(ctx, lockID, changes, opts)
}

func (c *Client) update(ctx context.Context, op string, body map[string]any) (*LoadedLock, error) {
	data, err := c.post(ctx, "/update", body)
	if err != nil {
		return nil, err
	}
	if present, _, reason := statusOf(data); present {
		if reason != "" {
			return nil, newAPIError(op, 200, nil, data)
		}
		// ChastiSafe uses the presence of a status member to mean "no lock
		// returned", not necessarily an error. The web client does the same.
		return nil, nil
	}
	var lock LoadedLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, newAPIError(op, 200, nil, data)
	}
	if lock.ID == "" {
		return nil, nil
	}
	return &lock, nil
}

// ClearFlags clears warning bits from a lock.
func (c *Client) ClearFlags(ctx context.Context, lockID ID, flags int) (*LoadedLock, error) {
	const op = "POST /clear"
	data, err := c.post(ctx, "/clear", map[string]any{"id": lockID, "flags": flags})
	if err != nil {
		return nil, err
	}
	return decodeLockOrStatus(op, data)
}

// Split creates fake locks from an active card lock.
func (c *Client) Split(ctx context.Context, lockID ID, count int) ([]LoadedLock, error) {
	const op = "POST /split"
	if count < 2 {
		return nil, errors.New("chastisafe: split count must be at least 2")
	}
	data, err := c.post(ctx, "/split", map[string]any{"lockid": lockID, "count": count})
	if err != nil {
		return nil, err
	}
	var out struct {
		Status Status       `json:"status"`
		Locks  []LoadedLock `json:"locks"`
		Reason string       `json:"reason"`
	}
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK || len(out.Locks) == 0 {
		return nil, newAPIError(op, 200, nil, data)
	}
	return out.Locks, nil
}

// Trust extends a lockee's trust period. duration is stored as seconds. This
// is a lockee action; ChastiSafe returns an internal error when a keyholder
// calls it.
func (c *Client) Trust(ctx context.Context, lockID ID, duration time.Duration) (*LoadedLock, error) {
	const op = "POST /trust"
	seconds, err := wholeSeconds(duration)
	if err != nil {
		return nil, err
	}
	body, err := c.post(ctx, "/trust", map[string]any{"lockId": lockID, "time": seconds})
	if err != nil {
		return nil, err
	}
	var out struct {
		Status Status      `json:"status"`
		Reason string      `json:"reason"`
		Lock   *LoadedLock `json:"lock"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return nil, newAPIError(op, 200, nil, body)
	}
	if out.Reason != "" || (out.Status != StatusOK && out.Lock == nil) {
		return nil, newAPIError(op, 200, nil, body)
	}
	if out.Lock != nil {
		return out.Lock, nil
	}
	var lock LoadedLock
	if err := json.Unmarshal(body, &lock); err == nil && lock.ID != "" {
		return &lock, nil
	}
	return nil, nil
}

// CheckIn records a periodic check-in.
func (c *Client) CheckIn(ctx context.Context, lockID ID) error {
	return c.action(ctx, "GET /checkin/{id}", "/checkin/"+url.PathEscape(lockID.String()))
}

// RequestKeyholderDecision asks the keyholder to decide whether the lock ends.
func (c *Client) RequestKeyholderDecision(ctx context.Context, lockID ID) error {
	return c.action(ctx, "GET /keyholderdecide/{id}", "/keyholderdecide/"+url.PathEscape(lockID.String()))
}

// HygieneUnlock starts a hygiene unlock with the lock's code.
func (c *Client) HygieneUnlock(ctx context.Context, lockID ID, code string) error {
	const op = "POST /hygiene"
	body, err := c.post(ctx, "/hygiene", map[string]any{"lockid": lockID, "code": code})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// HygieneAction runs the hygiene action for a lock.
func (c *Client) HygieneAction(ctx context.Context, lockID ID) error {
	return c.action(ctx, "GET /hygiene/{id}", "/hygiene/"+url.PathEscape(lockID.String()))
}

// HygieneSkip skips an available hygiene unlock.
func (c *Client) HygieneSkip(ctx context.Context, lockID ID) error {
	return c.action(ctx, "GET /hygieneskip/{id}", "/hygieneskip/"+url.PathEscape(lockID.String()))
}

// PutGreenBack returns the last green card to the deck.
func (c *Client) PutGreenBack(ctx context.Context, lockID ID) error {
	return c.action(ctx, "GET /putgreenback/{id}", "/putgreenback/"+url.PathEscape(lockID.String()))
}

// Respond answers a keyholder decision prompt.
func (c *Client) Respond(ctx context.Context, lockID ID, code, response string) error {
	const op = "POST /respond/{id}"
	body, err := c.post(ctx, "/respond/"+url.PathEscape(lockID.String()), map[string]any{
		"id":       lockID,
		"code":     code,
		"response": response,
	})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

func (c *Client) action(ctx context.Context, op, path string) error {
	body, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	var out ActionStatus
	if err := decodeJSON(body, &out); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// AssignTask gives a lockee a task from a roller.
func (c *Client) AssignTask(ctx context.Context, lockID, taskID ID) error {
	const op = "POST /taskassign"
	body, err := c.post(ctx, "/taskassign", map[string]any{"lockId": lockID, "taskId": taskID})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// PickTask draws a task for a lockee.
func (c *Client) PickTask(ctx context.Context, lockID ID) (*TaskPick, error) {
	const op = "GET /taskpick/{id}"
	body, err := c.get(ctx, "/taskpick/"+url.PathEscape(lockID.String()))
	if err != nil {
		return nil, err
	}
	var out TaskPick
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		return &out, newAPIError(op, 200, nil, body)
	}
	return &out, nil
}

// CompleteTask marks the current task complete.
func (c *Client) CompleteTask(ctx context.Context, lockID ID) (bool, error) {
	return c.boolAction(ctx, "GET /taskcomplete/{id}", "/taskcomplete/"+url.PathEscape(lockID.String()))
}

// AbandonTask abandons the current task.
func (c *Client) AbandonTask(ctx context.Context, lockID ID) (bool, error) {
	return c.boolAction(ctx, "GET /taskabandon/{id}", "/taskabandon/"+url.PathEscape(lockID.String()))
}

func (c *Client) boolAction(ctx context.Context, op, path string) (bool, error) {
	body, err := c.get(ctx, path)
	if err != nil {
		return false, err
	}
	var out struct {
		Status Status `json:"status"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	if out.Reason != "" {
		return false, newAPIError(op, 200, nil, body)
	}
	return out.Status != StatusOK, nil
}

// StartPillory starts a pillory session on a lock.
func (c *Client) StartPillory(ctx context.Context, lockID, taskRollerID ID) error {
	const op = "POST /startpillory"
	body, err := c.post(ctx, "/startpillory", map[string]any{
		"lockid":     lockID,
		"taskroller": taskRollerID,
	})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// Vote casts a pillory vote.
func (c *Client) Vote(ctx context.Context, lockID ID, vote int) error {
	const op = "POST /vote"
	body, err := c.post(ctx, "/vote", map[string]any{"lockid": lockID, "vote": vote})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// PilloryTaskDone marks the pillory task complete.
func (c *Client) PilloryTaskDone(ctx context.Context, lockID ID) error {
	const op = "POST /pillorytaskdone"
	body, err := c.post(ctx, "/pillorytaskdone", map[string]any{"lockid": lockID})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// CancelPillory cancels a pillory session.
func (c *Client) CancelPillory(ctx context.Context, lockID ID) error {
	return c.action(ctx, "GET /cancelpillory/{id}", "/cancelpillory/"+url.PathEscape(lockID.String()))
}

// RunScript executes a script against a loaded lock.
func (c *Client) RunScript(ctx context.Context, lockID ID, script string) error {
	const op = "POST /script"
	body, err := c.post(ctx, "/script", map[string]any{"lockId": lockID, "script": script})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// ValidateScript checks a script without running it.
func (c *Client) ValidateScript(ctx context.Context, script string) error {
	const op = "POST /validatescript"
	body, err := c.post(ctx, "/validatescript", map[string]any{"script": script})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// PressButton runs a script-backed button on a lock.
func (c *Client) PressButton(ctx context.Context, lockID, scriptID ID) (bool, error) {
	const op = "POST /button"
	body, err := c.post(ctx, "/button", map[string]any{"lockId": lockID, "scriptId": scriptID})
	if err != nil {
		return false, err
	}
	var out map[string]json.RawMessage
	if err := decodeJSON(body, &out); err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	var reason string
	if raw := out["reason"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &reason)
	}
	if strings.TrimSpace(reason) != "" {
		return false, newAPIError(op, 200, nil, body)
	}
	_, present := out["status"]
	return present, nil
}

// CreateScript adds a script to a template.
func (c *Client) CreateScript(ctx context.Context, script Script) error {
	const op = "POST /scripts"
	body, err := c.post(ctx, "/scripts", map[string]any{
		"templateId": script.TemplateID,
		"scriptId":   script.ScriptID,
		"name":       script.Name,
		"runAtLoad":  script.RunAtLoad,
		"scriptText": script.ScriptText,
	})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// UpdateScript changes a saved script.
func (c *Client) UpdateScript(ctx context.Context, script Script) error {
	const op = "POST /scripts/{template}/{script}"
	path := "/scripts/" + url.PathEscape(script.TemplateID.String()) + "/" + url.PathEscape(script.ScriptID.String())
	body, err := c.post(ctx, path, map[string]any{
		"name":       script.Name,
		"runAtLoad":  script.RunAtLoad,
		"scriptText": script.ScriptText,
	})
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// DeleteScript removes a saved script.
func (c *Client) DeleteScript(ctx context.Context, templateID, scriptID ID) error {
	const op = "DELETE /scripts/{template}/{script}"
	path := "/scripts/" + url.PathEscape(templateID.String()) + "/" + url.PathEscape(scriptID.String())
	body, err := c.do(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	if present, ok, _ := statusOf(body); present && !ok {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// CreateTemplate creates a lock template from a raw payload. The payload
// follows the field names in the web client, for example "lockname",
// "locktype", "timerMin" or "cards". If the payload contains an existing
// "id", ChastiSafe replaces that template in place.
func (c *Client) CreateTemplate(ctx context.Context, spec map[string]any) error {
	const op = "POST /create"
	body, err := c.post(ctx, "/create", spec)
	if err != nil {
		return err
	}
	if hasErrorMember(body) {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// UpdateTemplate applies a partial template update such as
// {"templateId": id, "description": "..."}. ChastiSafe reports only success or
// an error; call Template to read the result.
func (c *Client) UpdateTemplate(ctx context.Context, spec map[string]any) error {
	const op = "POST /updatetemplate"
	payload := make(map[string]any, len(spec)+1)
	for key, value := range spec {
		payload[key] = value
	}
	if _, ok := payload["templateId"]; !ok {
		if id, ok := payload["id"]; ok {
			payload["templateId"] = id
			delete(payload, "id")
		}
	}
	body, err := c.post(ctx, "/updatetemplate", payload)
	if err != nil {
		return err
	}
	if hasErrorMember(body) {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// DeleteTemplate deletes a lock template.
func (c *Client) DeleteTemplate(ctx context.Context, templateID ID) error {
	const op = "GET /delete/{id}"
	body, err := c.get(ctx, "/delete/"+url.PathEscape(templateID.String()))
	if err != nil {
		return err
	}
	if present, ok, reason := statusOf(body); present && (!ok || reason != "") {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// CardRange changes one card range on a template. GreensRequired, DiceCount,
// DiceFaces and PinkScript are only sent when set.
type CardRange struct {
	Card           Card
	Min            int
	Max            int
	GreensRequired *int
	DiceCount      *int
	DiceFaces      *int
	PinkScript     *string
}

// UpdateCardRange edits one card range on a template. ChastiSafe uses this
// endpoint for the card editor in the web app.
func (c *Client) UpdateCardRange(ctx context.Context, templateID ID, change CardRange) error {
	const op = "POST /updatecardrange"
	body := map[string]any{
		"templateId": templateID,
		"cardName":   change.Card,
		"minCount":   change.Min,
		"maxCount":   change.Max,
	}
	switch change.Card {
	case CardGreen:
		if change.GreensRequired != nil {
			body["greensRequired"] = *change.GreensRequired
		}
	case CardDice:
		if change.DiceCount != nil {
			body["diceCount"] = *change.DiceCount
		}
		if change.DiceFaces != nil {
			body["diceFaces"] = *change.DiceFaces
		}
	case CardPink:
		if change.PinkScript != nil {
			body["pinkScript"] = *change.PinkScript
		}
	}
	data, err := c.post(ctx, "/updatecardrange", body)
	if err != nil {
		return err
	}
	if hasErrorMember(data) {
		return newAPIError(op, 200, nil, data)
	}
	return nil
}

// SetBackground changes a template's background image.
func (c *Client) SetBackground(ctx context.Context, templateID ID, background string) error {
	const op = "POST /setbackground"
	body, err := c.post(ctx, "/setbackground", map[string]any{
		"templateId": templateID,
		"background": background,
	})
	if err != nil {
		return err
	}
	if present, _, reason := statusOf(body); present && reason != "" {
		return newAPIError(op, 200, nil, body)
	}
	return nil
}

// Simulation is the statistical preview of a card template.
type Simulation struct {
	AvgDuration         int    `json:"avgduration"`
	MinDuration         int    `json:"minduration"`
	MaxDuration         int    `json:"maxduration"`
	AvgCards            int    `json:"avgcards"`
	MinCards            int    `json:"mincards"`
	MaxCards            int    `json:"maxcards"`
	Regularity          int    `json:"regularity"`
	MinimumDuration     int    `json:"minimumduration"`
	NumberOfSimulations int    `json:"numberOfSimulations"`
	Status              Status `json:"status"`
}

// Simulate previews a card template's duration and card counts.
func (c *Client) Simulate(ctx context.Context, templateID ID) (*Simulation, error) {
	const op = "POST /simulate"
	body, err := c.post(ctx, "/simulate", map[string]any{"templateid": templateID})
	if err != nil {
		return nil, err
	}
	var out Simulation
	if err := decodeJSON(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if out.Status != StatusOK {
		return &out, newAPIError(op, 200, nil, body)
	}
	return &out, nil
}

// hasErrorMember reports whether a response has a meaningful error member.
// A zero, false, null or empty error is success.
func hasErrorMember(data []byte) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return true
	}
	raw, ok := payload["error"]
	if !ok {
		return false
	}
	value := strings.TrimSpace(string(raw))
	switch strings.ToLower(strings.Trim(value, `"`)) {
	case "", "0", "false", "null":
		return false
	}
	return true
}

func decodeLockOrStatus(op string, data []byte) (*LoadedLock, error) {
	if present, _, reason := statusOf(data); present {
		if reason != "" {
			return nil, newAPIError(op, 200, nil, data)
		}
		// ChastiSafe uses the presence of a status member to mean "no lock
		// returned", not necessarily an error. The web client does the same.
		return nil, nil
	}
	var lock LoadedLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, newAPIError(op, 200, nil, data)
	}
	if lock.ID == "" {
		return nil, nil
	}
	return &lock, nil
}

func wholeSeconds(duration time.Duration) (int, error) {
	seconds := int64(duration / time.Second)
	if duration%time.Second != 0 {
		return 0, fmt.Errorf("chastisafe: %s is not a whole number of seconds", duration)
	}
	return int(seconds), nil
}
