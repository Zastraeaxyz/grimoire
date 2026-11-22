package chastisafe

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors. Use errors.Is on any error returned by this package.
var (
	ErrUnauthorized    = errors.New("chastisafe: unauthorized")
	ErrLockNotFound    = errors.New("chastisafe: lock not found")
	ErrLockNotReady    = errors.New("chastisafe: lock is not ready")
	ErrNotKeyholder    = errors.New("chastisafe: caller is not the keyholder")
	ErrNotAllowed      = errors.New("chastisafe: action is not allowed")
	ErrInvalidResponse = errors.New("chastisafe: invalid API response")
)

// APIError is an error response from ChastiSafe. The server often returns
// HTTP 200 with a JSON status field, so a non-zero status is also represented
// here.
type APIError struct {
	Op         string
	HTTPStatus int
	Status     string
	Reason     string
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	op := e.Op
	if op == "" {
		op = "request"
	}
	switch {
	case e.Reason != "":
		return fmt.Sprintf("chastisafe: %s: %s", op, e.Reason)
	case e.Message != "":
		return fmt.Sprintf("chastisafe: %s: %s", op, e.Message)
	case e.HTTPStatus != 0:
		return fmt.Sprintf("chastisafe: %s: HTTP %d", op, e.HTTPStatus)
	default:
		return fmt.Sprintf("chastisafe: %s failed", op)
	}
}

// Is maps server reasons onto the package's sentinel errors.
func (e *APIError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch target {
	case ErrUnauthorized:
		return e.HTTPStatus == http.StatusUnauthorized ||
			e.HTTPStatus == http.StatusForbidden ||
			oneOf(e.Reason, "unauthorized", "notloggedin", "loginrequired", "notsignedin")
	case ErrLockNotFound:
		return e.Reason == "locknotfound"
	case ErrNotKeyholder:
		return oneOf(e.Reason, "notkeyholder", "keyholderonly", "notthekeyholder")
	case ErrLockNotReady:
		return oneOf(e.Reason, "notready", "notunlockable", "locknotready", "notyet")
	case ErrNotAllowed:
		return oneOf(e.Reason,
			"nodoublelocking", "requestedpurposenotallowed", "locknotavailable",
			"passcodemismatch", "inactivelockee", "notallowed", "cardinfohidden",
		)
	case ErrInvalidResponse:
		return false
	}
	return false
}

// IsReason reports whether err is an APIError with the exact server reason.
func IsReason(err error, reason string) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Reason == reason
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

type errorPayload struct {
	Status  json.RawMessage `json:"status"`
	Reason  string          `json:"reason"`
	Error   string          `json:"error"`
	Message string          `json:"message"`
}

func newAPIError(op string, httpStatus int, header http.Header, body []byte) *APIError {
	apiErr := &APIError{
		Op:         op,
		HTTPStatus: httpStatus,
	}
	retryAfter := strings.TrimSpace(header.Get("Retry-After"))
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
			apiErr.RetryAfter = time.Duration(seconds) * time.Second
		} else if when, err := http.ParseTime(retryAfter); err == nil {
			if delay := time.Until(when); delay > 0 {
				apiErr.RetryAfter = delay
			}
		}
	}

	var payload errorPayload
	if err := json.Unmarshal(body, &payload); err == nil {
		apiErr.Status = rawStatus(payload.Status)
		apiErr.Reason = strings.TrimSpace(payload.Reason)
		apiErr.Message = strings.TrimSpace(payload.Error)
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(payload.Message)
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = shortBody(body)
	}
	return apiErr
}

func rawStatus(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	return text
}

// statusOf inspects a JSON response's status member. ok is true when status is
// present and represents success (0 or false).
func statusOf(body []byte) (present bool, ok bool, reason string) {
	var payload struct {
		Status json.RawMessage `json:"status"`
		Reason string          `json:"reason"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, false, ""
	}
	if len(payload.Status) == 0 {
		return false, false, strings.TrimSpace(payload.Reason)
	}
	switch strings.ToLower(rawStatus(payload.Status)) {
	case "", "0", "false", "ok", "success":
		return true, true, strings.TrimSpace(payload.Reason)
	default:
		return true, false, strings.TrimSpace(payload.Reason)
	}
}

func shortBody(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if text == "" {
		return ""
	}
	if len(text) > maxErrorBodyChars {
		text = text[:maxErrorBodyChars] + "..."
	}
	return text
}
