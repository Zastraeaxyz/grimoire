package bdsmlr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const profileFixture = `<!doctype html><html><body>
<form name="profile" id="your-profile" action="profile.php" method="post">
<input type="text" name="user_login" id="user_login" value="testmember" readonly="readonly" />
</form>
</body></html>`

const sendFormFixture = `<!doctype html><html><body>
<aside class="bp-feedback bp-messages info">
<span class="bp-icon" aria-hidden="true"></span>
<p>Fill out the form below to invite a new user to join this site.</p>
</aside>
<form class="standard-form network-invitation-form" id="network-invitation-form" method="post">
<label for="bp_members_invitation_invitee_email">Email</label>
<input id="bp_members_invitation_invitee_email" type="email" name="invitee_email" required="required">
<label for="bp_members_invitation_message">Message</label>
<textarea id="bp_members_invitation_message" name="content"></textarea>
<input type="hidden" name="action" value="send-invite">
<div class="submit"><input type="submit" name="member-send-invite-submit" id="submit" value="Send" class="auto"/></div>
<input type="hidden" id="_wpnonce" name="_wpnonce" value="test-nonce" />
<input type="hidden" name="_wp_http_referer" value="/members/testmember/invitations/" />
</form>
</body></html>`

const successFixture = `<!doctype html><html><body>
<aside class="bp-feedback bp-messages bp-template-notice success">
<span class="bp-icon" aria-hidden="true"></span>
<p>Invitation successfully sent!</p>
</aside>
</body></html>`

const errorFixture = `<!doctype html><html><body>
<aside class="bp-feedback bp-messages error">
<span class="bp-icon" aria-hidden="true"></span>
<p>An invitation has already been sent to that email address.</p>
</aside>
</body></html>`

func newTestFeature(t *testing.T, handler http.Handler) *Feature {
	t.Helper()

	srv := httptest.NewServer(handler)
	oldBase := accountBaseURL
	accountBaseURL = srv.URL
	t.Cleanup(func() {
		accountBaseURL = oldBase
		srv.Close()
	})

	return New(Options{
		GuildID: "guild-1",
		Session: "wordpress_logged_in_test=testmember%7C1795649467%7Ctoken%7Chmac; wordpress_sec_test=testmember%7C1795649467%7Ctoken2%7Chmac2; v2_session=ignored",
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestSendInvite(t *testing.T) {
	const login = "testmember"
	wantPath := "/members/" + login + "/invitations/"

	var posted url.Values
	var postHeaders http.Header
	sent := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/members/"+login+"/invitations/send-invites/":
			http.Redirect(w, r, wantPath, http.StatusMovedPermanently)

		case r.URL.Path == wantPath && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			posted = r.PostForm
			postHeaders = r.Header.Clone()
			sent = true
			http.Redirect(w, r, "/members/"+login+"/invitations/send-invites/", http.StatusFound)

		case r.URL.Path == wantPath && sent:
			_, _ = io.WriteString(w, successFixture)

		case r.URL.Path == wantPath:
			_, _ = io.WriteString(w, sendFormFixture)

		default:
			http.NotFound(w, r)
		}
	})

	f := newTestFeature(t, handler)
	message, err := f.sendInvite(context.Background(), "invitee+tag@example.com")
	if err != nil {
		t.Fatalf("sendInvite: %v", err)
	}
	if message != "Invitation successfully sent!" {
		t.Fatalf("message = %q, want the success notice", message)
	}

	if posted.Get("invitee_email") != "invitee+tag@example.com" {
		t.Errorf("invitee_email = %q", posted.Get("invitee_email"))
	}
	if posted.Get("content") != "" {
		t.Errorf("content = %q, want empty", posted.Get("content"))
	}
	if posted.Get("action") != "send-invite" {
		t.Errorf("action = %q", posted.Get("action"))
	}
	if posted.Get("member-send-invite-submit") != "Send" {
		t.Errorf("submit = %q", posted.Get("member-send-invite-submit"))
	}
	if posted.Get("_wpnonce") != "test-nonce" {
		t.Errorf("nonce = %q", posted.Get("_wpnonce"))
	}
	if posted.Get("_wp_http_referer") != wantPath {
		t.Errorf("referer = %q, want %q", posted.Get("_wp_http_referer"), wantPath)
	}

	if got := postHeaders.Get("Cookie"); got != "wordpress_logged_in_test=testmember%7C1795649467%7Ctoken%7Chmac; wordpress_sec_test=testmember%7C1795649467%7Ctoken2%7Chmac2" {
		t.Errorf("Cookie = %q", got)
	}
	if got := postHeaders.Get("User-Agent"); got != accountUserAgent {
		t.Errorf("User-Agent = %q", got)
	}
	if got := postHeaders.Get("Origin"); got == "" {
		t.Error("Origin header is missing")
	}
	if got := postHeaders.Get("Referer"); !strings.HasSuffix(got, wantPath) {
		t.Errorf("Referer = %q", got)
	}
	if got := postHeaders.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestSendInviteSessionExpired(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/wp-login.php?redirect_to=invitations&action=bpnoaccess", http.StatusFound)
	})

	f := newTestFeature(t, handler)
	if _, err := f.sendInvite(context.Background(), "a@example.com"); !errors.Is(err, errAccountSession) {
		t.Fatalf("err = %v, want errAccountSession", err)
	}
}

func TestSendInviteReportsErrorNotice(t *testing.T) {
	const login = "testmember"
	path := "/members/" + login + "/invitations/"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == path && r.Method == http.MethodPost:
			http.Redirect(w, r, path, http.StatusFound)
		case r.URL.Path == path:
			if r.Method == http.MethodPost || r.Header.Get("Referer") != "" {
				_, _ = io.WriteString(w, errorFixture)
				return
			}
			_, _ = io.WriteString(w, sendFormFixture)
		default:
			http.NotFound(w, r)
		}
	})

	f := newTestFeature(t, handler)
	_, err := f.sendInvite(context.Background(), "a@example.com")
	if err == nil || !strings.Contains(err.Error(), "already been sent") {
		t.Fatalf("err = %v, want the error notice", err)
	}
}

func TestSendInviteNeedsForm(t *testing.T) {
	const login = "testmember"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body><p>Nothing here.</p></body></html>`)
	})

	f := newTestFeature(t, handler)
	if _, err := f.sendInvite(context.Background(), "a@example.com"); err == nil {
		t.Fatal("expected an error for a page without the invitation form")
	}
}

func TestNormalizeAccountCookie(t *testing.T) {
	full := "Cookie: wordpress_logged_in_abc=value; wordpress_sec_abc=value; v2_session=secret"
	want := "wordpress_logged_in_abc=value; wordpress_sec_abc=value"
	if got := normalizeAccountCookie(full); got != want {
		t.Fatalf("normalizeAccountCookie(full) = %q, want %q", got, want)
	}

	bare := "user|1700000000|token|hmac"
	got := normalizeAccountCookie(bare)
	if !strings.Contains(got, "wordpress_logged_in_") {
		t.Fatalf("normalizeAccountCookie(bare) = %q, want wordpress_logged_in_ names", got)
	}
	if strings.Count(got, "wordpress_logged_in_") != 4 {
		t.Fatalf("normalizeAccountCookie(bare) = %q, want four candidates", got)
	}

	if got := normalizeAccountCookie("v2_session=secret"); got != "" {
		t.Fatalf("normalizeAccountCookie(v2 only) = %q, want empty", got)
	}
	if got := normalizeAccountCookie(""); got != "" {
		t.Fatalf("normalizeAccountCookie(empty) = %q, want empty", got)
	}
}

func TestValidateEmail(t *testing.T) {
	for _, email := range []string{"a@example.com", "invitee+tag@example.com"} {
		if _, err := validateEmail(email); err != nil {
			t.Errorf("validateEmail(%q): %v", email, err)
		}
	}
	for _, email := range []string{"", "not-an-email", "Name <a@example.com>", "a@example.com b"} {
		if _, err := validateEmail(email); err == nil {
			t.Errorf("validateEmail(%q) unexpectedly succeeded", email)
		}
	}
}

func TestUserLoginFromCookie(t *testing.T) {
	cases := []struct {
		name   string
		cookie string
		want   string
		ok     bool
	}{
		{
			name:   "url encoded",
			cookie: "wordpress_logged_in_test=testmember%7C1795649467%7Ctoken%7Chmac",
			want:   "testmember",
			ok:     true,
		},
		{
			name:   "raw pipes",
			cookie: "wordpress_logged_in_test=someone|1795649467|token|hmac",
			want:   "someone",
			ok:     true,
		},
		{
			name:   "missing logged in cookie",
			cookie: "wordpress_sec_test=someone%7C1795649467%7Ctoken%7Chmac",
			ok:     false,
		},
		{
			name:   "empty name",
			cookie: "wordpress_logged_in_test=%7C1795649467%7Ctoken%7Chmac",
			ok:     false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := userLoginFromCookie(tc.cookie)
			if tc.ok && err != nil {
				t.Fatalf("userLoginFromCookie: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected an error")
			}
			if got != tc.want {
				t.Fatalf("userLoginFromCookie = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFollowRedirectsRejectsForeignHost(t *testing.T) {
	f := newTestFeature(t, http.NotFoundHandler())
	page := accountPage{
		status:   http.StatusFound,
		location: "https://example.com/steal",
		url:      accountBaseURL + "/start",
	}
	if _, err := f.followRedirects(context.Background(), page); err == nil {
		t.Fatal("expected a foreign-host redirect to fail")
	}
}

func TestFeedbackKind(t *testing.T) {
	cases := map[string]string{
		"bp-feedback bp-messages success":                    "success",
		"bp-feedback bp-messages error":                      "error",
		"notice notice-warning":                              "error",
		"bp-feedback bp-messages info":                       "info",
		"bp-feedback bp-messages bp-template-notice updated": "success",
	}
	for classes, want := range cases {
		if got := feedbackKind(classes); got != want {
			t.Errorf("feedbackKind(%q) = %q, want %q", classes, got, want)
		}
	}
}

func TestSendInviteUsesFreshNonce(t *testing.T) {
	// A second call must re-read the page instead of reusing the first nonce.
	const login = "testmember"
	path := "/members/" + login + "/invitations/"
	nonce := 0

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == path && r.Method == http.MethodPost:
			if r.PostFormValue("_wpnonce") != fmt.Sprintf("nonce-%d", nonce) {
				t.Errorf("nonce = %q, want nonce-%d", r.PostFormValue("_wpnonce"), nonce)
			}
			_, _ = io.WriteString(w, successFixture)
		case r.URL.Path == path:
			nonce++
			_, _ = fmt.Fprintf(w, `<html><body><form id="network-invitation-form" method="post">
<input type="email" name="invitee_email">
<input type="hidden" name="_wpnonce" value="nonce-%d">
</form></body></html>`, nonce)
		default:
			http.NotFound(w, r)
		}
	})

	f := newTestFeature(t, handler)
	for i := 1; i <= 2; i++ {
		if _, err := f.sendInvite(context.Background(), "a@example.com"); err != nil {
			t.Fatalf("sendInvite #%d: %v", i, err)
		}
	}
}
