package bdsmlr

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	xhtml "golang.org/x/net/html"
)

// accountPage is one response with its body already read.
type accountPage struct {
	status   int
	location string
	body     []byte
	url      string
}

// feedback is a BuddyPress or WordPress notice on an account page.
type feedback struct {
	kind    string
	message string
}

// sendForm is the BuddyPress invitation form.
type sendForm struct {
	allowed bool
	nonce   string
	fields  map[string]string
}

// sendInvite loads a fresh nonce, posts the invitation, and reports what the
// account site said. The session cookie is never logged.
func (f *Feature) sendInvite(ctx context.Context, email string) (string, error) {
	if f.cookie == "" {
		return "", errAccountSession
	}

	login, err := f.ensureUserLogin(ctx)
	if err != nil {
		return "", err
	}

	path := "/members/" + url.PathEscape(login) + "/invitations/"
	target, err := accountURL(path)
	if err != nil {
		return "", err
	}

	page, err := f.fetchPage(ctx, target, "")
	if err != nil {
		return "", err
	}
	if err := assertPage(page); err != nil {
		return "", err
	}

	doc, err := parseHTML(page.body)
	if err != nil {
		return "", fmt.Errorf("reading the invitation form: %w", err)
	}
	form, feedback := parseSendForm(doc)
	if !form.allowed {
		if feedback != nil && feedback.kind == "error" {
			return "", errors.New(feedback.message)
		}
		return "", errors.New("the invitation form was not found; the session may belong to a different account")
	}
	if form.nonce == "" {
		return "", errors.New("the invitation form had no security token; refresh `BDSMLR_SESSION`")
	}

	// Post to the page that actually served the form. If the account slug
	// differed from the login name, the GET above followed the redirect.
	formURL := page.url
	if formURL == "" {
		formURL = target
	}

	values := url.Values{}
	for name, value := range form.fields {
		values.Set(name, value)
	}
	values.Set("action", "send-invite")
	values.Set("invitee_email", email)
	values.Set("content", "")
	values.Set("member-send-invite-submit", "Send")
	if values.Get("_wp_http_referer") == "" {
		if parsed, err := url.Parse(formURL); err == nil {
			values.Set("_wp_http_referer", parsed.Path)
		} else {
			values.Set("_wp_http_referer", path)
		}
	}

	result, err := f.request(ctx, http.MethodPost, formURL, values, formURL)
	if err != nil {
		return "", err
	}
	result, err = f.followRedirects(ctx, result)
	if err != nil {
		return "", err
	}
	if err := assertPage(result); err != nil {
		return "", err
	}

	resultDoc, err := parseHTML(result.body)
	if err != nil {
		return "", fmt.Errorf("reading the invitation result: %w", err)
	}
	if notice := parseFeedback(resultDoc); notice != nil {
		if notice.kind == "error" {
			return "", errors.New(notice.message)
		}
		if notice.kind == "success" {
			return notice.message, nil
		}
	}

	// No notice: a 2xx or a followable redirect means WordPress accepted it.
	return "Invitation sent.", nil
}

// ensureUserLogin reads the account username from the signed-in profile and
// caches it. The invitations URL needs the username, and it is not configured
// separately.
func (f *Feature) ensureUserLogin(ctx context.Context) (string, error) {
	f.userMu.Lock()
	defer f.userMu.Unlock()

	if f.userLogin != "" {
		return f.userLogin, nil
	}

	target, err := accountURL("/wp-admin/profile.php")
	if err != nil {
		return "", err
	}
	page, err := f.fetchPage(ctx, target, "")
	if err != nil {
		return "", err
	}
	if err := assertPage(page); err != nil {
		return "", err
	}

	login := parseUserLogin(page.body)
	if login == "" {
		return "", errors.New("could not read the account username from the session; copy a fresh cookie header from account.bdsmlr.com")
	}
	f.userLogin = login
	return login, nil
}

// request performs one account-site request. form is nil for GET requests.
func (f *Feature) request(ctx context.Context, method, rawURL string, form url.Values, referer string) (accountPage, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var body io.Reader
	contentType := ""
	if form != nil {
		body = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return accountPage{}, fmt.Errorf("building the bdsmlr request: %w", err)
	}
	req.Header.Set("User-Agent", accountUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Origin", accountOrigin())
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if f.cookie != "" {
		req.Header.Set("Cookie", f.cookie)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return accountPage{}, fmt.Errorf("contacting the bdsmlr account site: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return accountPage{}, fmt.Errorf("reading the bdsmlr response: %w", err)
	}
	if len(data) > maxPageBytes {
		return accountPage{}, fmt.Errorf("the bdsmlr response is larger than %d bytes", maxPageBytes)
	}

	pageURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		pageURL = resp.Request.URL.String()
	}
	return accountPage{
		status:   resp.StatusCode,
		location: resp.Header.Get("Location"),
		body:     data,
		url:      pageURL,
	}, nil
}

// fetchPage performs a GET and follows same-origin redirects.
func (f *Feature) fetchPage(ctx context.Context, rawURL, referer string) (accountPage, error) {
	page, err := f.request(ctx, http.MethodGet, rawURL, nil, referer)
	if err != nil {
		return accountPage{}, err
	}
	return f.followRedirects(ctx, page)
}

// followRedirects walks up to maxRedirects same-origin redirects. A redirect to
// the WordPress login page is treated as an expired session.
func (f *Feature) followRedirects(ctx context.Context, page accountPage) (accountPage, error) {
	for hops := 0; page.status >= 300 && page.status < 400; hops++ {
		if page.location == "" {
			return page, nil
		}
		if isLoginLocation(page.location) {
			return page, errAccountSession
		}
		if hops >= maxRedirects {
			return page, errors.New("the bdsmlr account site redirected too many times")
		}
		next, err := resolveAccountURL(page.url, page.location)
		if err != nil {
			return page, err
		}
		page, err = f.request(ctx, http.MethodGet, next, nil, page.url)
		if err != nil {
			return accountPage{}, err
		}
	}
	return page, nil
}

// assertPage turns the account site's error responses into readable errors.
func assertPage(page accountPage) error {
	if isLoginLocation(page.location) {
		return errAccountSession
	}
	if page.status == http.StatusOK && isLoginPage(page.body) {
		return errAccountSession
	}

	switch {
	case page.status == http.StatusUnauthorized:
		return errAccountSession
	case page.status == http.StatusTooManyRequests:
		return errors.New("bdsmlr is rate limiting the request; try again in a minute")
	case page.status == 419:
		return errors.New("the bdsmlr security token expired; try again")
	case page.status == http.StatusForbidden || page.status == http.StatusServiceUnavailable:
		if isCloudflareBlock(page.body) {
			return errors.New("bdsmlr blocked the request; try again in a minute")
		}
		return fmt.Errorf("the bdsmlr account site returned HTTP %d", page.status)
	case page.status == http.StatusNotFound:
		return errors.New("the bdsmlr account page was not found")
	case page.status >= 500:
		return fmt.Errorf("the bdsmlr account site returned HTTP %d", page.status)
	case page.status < 200 || page.status >= 300:
		return fmt.Errorf("the bdsmlr account site returned HTTP %d", page.status)
	}
	return nil
}

// isLoginLocation recognizes the WordPress login redirect, including the
// bpnoaccess marker used for a signed-out BuddyPress member page.
func isLoginLocation(location string) bool {
	lower := strings.ToLower(location)
	return strings.Contains(lower, "wp-login.php") || strings.Contains(lower, "bpnoaccess")
}

func isCloudflareBlock(body []byte) bool {
	text := strings.ToLower(string(body))
	return strings.Contains(text, "cf-chl") ||
		strings.Contains(text, "just a moment") ||
		strings.Contains(text, "challenges.cloudflare.com") ||
		strings.Contains(text, "attention required")
}

// isLoginPage recognizes the WordPress sign-in form served without a redirect.
func isLoginPage(body []byte) bool {
	return strings.Contains(strings.ToLower(string(body)), `id="loginform"`)
}

// accountURL resolves a path against the configured account base.
func accountURL(path string) (string, error) {
	base, err := url.Parse(accountBaseURL)
	if err != nil {
		return "", fmt.Errorf("invalid bdsmlr account base: %w", err)
	}
	ref, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid bdsmlr path: %w", err)
	}
	return base.ResolveReference(ref).String(), nil
}

// resolveAccountURL resolves a redirect target and keeps it on the account
// host, so the session cookie is never sent elsewhere.
func resolveAccountURL(baseURL, location string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid bdsmlr URL: %w", err)
	}
	ref, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("invalid bdsmlr redirect: %w", err)
	}
	next := base.ResolveReference(ref)
	if next.Scheme+"://"+next.Host != accountOrigin() {
		return "", errors.New("the bdsmlr account site redirected to an unexpected host")
	}
	return next.String(), nil
}

func accountOrigin() string {
	base, err := url.Parse(accountBaseURL)
	if err != nil || base.Host == "" {
		return accountBase
	}
	return base.Scheme + "://" + base.Host
}

// normalizeAccountCookie keeps only the WordPress auth cookies. A full Cookie
// header from account.bdsmlr.com wins. A bare cookie value is expanded into
// the candidate wordpress_logged_in_ names, matching WordPress's md5(origin)
// naming scheme. Note that a bare value cannot sign in to wp-admin pages,
// which need wordpress_sec_ as well.
func normalizeAccountCookie(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if name, value, ok := strings.Cut(raw, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "cookie") {
		raw = strings.TrimSpace(value)
	}

	pairs := accountCookiePairs(raw)
	if len(pairs) > 0 {
		return joinCookiePairs(pairs)
	}

	// A bare cookie value has no "name=" pairs and no whitespace. WordPress
	// values contain pipes, so only separators that would break a header are
	// rejected.
	if !strings.ContainsAny(raw, "=; \t\r\n") {
		for _, name := range candidateLoginCookieNames() {
			pairs = append(pairs, cookiePair{name: name, value: raw})
		}
		return joinCookiePairs(pairs)
	}
	return ""
}

type cookiePair struct {
	name  string
	value string
}

func accountCookiePairs(raw string) []cookiePair {
	var pairs []cookiePair
	seen := make(map[string]struct{})
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" || !isAccountCookieName(name) {
			continue
		}
		if strings.ContainsAny(value, "\r\n;") {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		pairs = append(pairs, cookiePair{name: name, value: value})
	}
	return pairs
}

func isAccountCookieName(name string) bool {
	return strings.HasPrefix(name, "wordpress_logged_in_") ||
		strings.HasPrefix(name, "wordpress_sec_") ||
		name == "wordpress_test_cookie"
}

// candidateLoginCookieNames mirrors WordPress's cookie hash for the account
// origin. The value is the same for every variant, so sending all of them
// covers http/https and a trailing-slash siteurl.
func candidateLoginCookieNames() []string {
	origins := []string{
		"https://account.bdsmlr.com",
		"https://account.bdsmlr.com/",
		"http://account.bdsmlr.com",
		"http://account.bdsmlr.com/",
	}
	names := make([]string, 0, len(origins))
	for _, origin := range origins {
		sum := md5.Sum([]byte(origin))
		names = append(names, fmt.Sprintf("wordpress_logged_in_%x", sum))
	}
	return names
}

func joinCookiePairs(pairs []cookiePair) string {
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, pair.name+"="+pair.value)
	}
	return strings.Join(parts, "; ")
}

// validateEmail accepts a bare address only. A display name would let a
// crafted value reach the form as something other than an email.
func validateEmail(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("Give me an email address to invite.")
	}
	if len(raw) > 254 {
		return "", errors.New("That email address is too long.")
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return "", errors.New("That is not a valid email address.")
	}
	if !strings.EqualFold(addr.Address, raw) {
		return "", errors.New("Give me a bare email address, without a display name.")
	}
	return addr.Address, nil
}

// parseHTML parses an account page.
func parseHTML(body []byte) (*xhtml.Node, error) {
	return xhtml.Parse(bytes.NewReader(body))
}

// parseSendForm finds the BuddyPress invitation form and reads its hidden
// fields. The nonce is per-session, so it must come from the live page.
func parseSendForm(doc *xhtml.Node) (sendForm, *feedback) {
	form := findNode(doc, func(n *xhtml.Node) bool {
		if n.Type != xhtml.ElementNode || n.Data != "form" {
			return false
		}
		return attrByName(n, "id") == "network-invitation-form" || hasInputNamed(n, "invitee_email")
	})

	parsed := sendForm{fields: make(map[string]string)}
	if form != nil {
		parsed.allowed = hasInputNamed(form, "invitee_email")
		walkNodes(form, func(n *xhtml.Node) {
			if n.Type != xhtml.ElementNode || n.Data != "input" {
				return
			}
			if !strings.EqualFold(attrByName(n, "type"), "hidden") {
				return
			}
			name := attrByName(n, "name")
			if name != "" {
				parsed.fields[name] = attrByName(n, "value")
			}
		})
		parsed.nonce = parsed.fields["_wpnonce"]
	}

	return parsed, parseFeedback(doc)
}

// parseFeedback reads the first meaningful BuddyPress or WordPress notice.
// Info-only help text is skipped.
func parseFeedback(doc *xhtml.Node) *feedback {
	var found *feedback
	walkNodes(doc, func(n *xhtml.Node) {
		if found != nil || n.Type != xhtml.ElementNode {
			return
		}
		if n.Data != "aside" && n.Data != "div" {
			return
		}

		classes := attrByName(n, "class")
		isMessage := attrByName(n, "id") == "message"
		if !hasClass(classes, "bp-feedback") && !isMessage && !hasClass(classes, "notice") {
			return
		}
		if isMessage && classes == "" {
			if child := firstElementChild(n); child != nil {
				classes = attrByName(child, "class")
			}
		}

		kind := feedbackKind(classes)
		if kind == "" || kind == "info" {
			return
		}
		message := collapseSpace(textContent(n))
		if message == "" {
			return
		}
		found = &feedback{kind: kind, message: message}
	})
	return found
}

func feedbackKind(classes string) string {
	lower := strings.ToLower(classes)
	switch {
	case strings.Contains(lower, "error"),
		strings.Contains(lower, "warning"),
		strings.Contains(lower, "danger"):
		return "error"
	case strings.Contains(lower, "success"),
		strings.Contains(lower, "updated"):
		return "success"
	case strings.Contains(lower, "info"):
		return "info"
	}
	return ""
}

// parseUserLogin reads the username from the WordPress profile form, with a
// canonical member link as a fallback.
func parseUserLogin(body []byte) string {
	doc, err := parseHTML(body)
	if err != nil {
		return ""
	}

	login := ""
	canonical := ""
	walkNodes(doc, func(n *xhtml.Node) {
		if n.Type != xhtml.ElementNode {
			return
		}
		if n.Data == "input" && attrByName(n, "id") == "user_login" && login == "" {
			login = strings.TrimSpace(attrByName(n, "value"))
		}
		if n.Data == "link" && strings.EqualFold(attrByName(n, "rel"), "canonical") && canonical == "" {
			canonical = attrByName(n, "href")
		}
	})
	if login != "" {
		return login
	}

	u, err := url.Parse(canonical)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "members" {
		return ""
	}
	if decoded, err := url.PathUnescape(parts[1]); err == nil {
		return decoded
	}
	return parts[1]
}

// --- small DOM helpers ---

func hasInputNamed(n *xhtml.Node, name string) bool {
	return findNode(n, func(x *xhtml.Node) bool {
		return x.Type == xhtml.ElementNode &&
			x.Data == "input" &&
			attrByName(x, "name") == name
	}) != nil
}

func findNode(n *xhtml.Node, pred func(*xhtml.Node) bool) *xhtml.Node {
	if pred(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, pred); found != nil {
			return found
		}
	}
	return nil
}

func walkNodes(n *xhtml.Node, fn func(*xhtml.Node)) {
	fn(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkNodes(child, fn)
	}
}

func firstElementChild(n *xhtml.Node) *xhtml.Node {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode {
			return child
		}
	}
	return nil
}

func hasClass(classes, want string) bool {
	for _, class := range strings.Fields(classes) {
		if class == want {
			return true
		}
	}
	return false
}

func attrByName(n *xhtml.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func textContent(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.TextNode {
			b.WriteString(x.Data)
		}
		if x.Type == xhtml.ElementNode && (x.Data == "script" || x.Data == "style") {
			return
		}
		for child := x.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
