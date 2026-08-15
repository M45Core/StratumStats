package app

import (
	"crypto/pbkdf2"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	adminCookieName       = "stratumstats_admin"
	adminSessionLifetime  = 12 * time.Hour
	adminPBKDF2Iterations = 600_000
	adminMaxLoginFailures = 5
	adminFailureWindow    = 5 * time.Minute
)

type adminCredentials struct {
	Version        int    `json:"version"`
	Username       string `json:"username"`
	PasswordKDF    string `json:"password_kdf"`
	Iterations     int    `json:"iterations"`
	PasswordSalt   string `json:"password_salt"`
	PasswordHash   string `json:"password_hash"`
	passwordSalt   []byte
	passwordDigest []byte
}

type adminSession struct {
	csrf      string
	expiresAt time.Time
}

type loginFailures struct {
	count       int
	windowStart time.Time
}

type adminServer struct {
	credentials adminCredentials
	load        func() ([]byte, string, error)
	save        func([]byte) (string, error)
	login       *template.Template
	editor      *template.Template
	mu          sync.Mutex
	sessions    map[string]adminSession
	failures    map[string]loginFailures
	now         func() time.Time
}

type adminEditorPage struct {
	CSRFToken string
	PoolsJSON string
	Revision  string
	Message   string
	Error     string
}

func newAdminHandler(path string, load func() ([]byte, string, error), save func([]byte) (string, error)) (http.Handler, string, error) {
	credentials, generatedPassword, err := loadOrCreateAdminCredentials(path)
	if err != nil {
		return nil, "", err
	}
	server := &adminServer{
		credentials: credentials, load: load, save: save,
		login:    template.Must(template.New("login").Parse(adminLoginHTML)),
		editor:   template.Must(template.New("editor").Parse(adminEditorHTML)),
		sessions: make(map[string]adminSession), failures: make(map[string]loginFailures),
	}
	return server, generatedPassword, nil
}

func loadOrCreateAdminCredentials(path string) (adminCredentials, string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return adminCredentials{}, "", err
	}
	encoded, err := os.ReadFile(path) // #nosec G304 -- the operator selects the credential path.
	if err == nil {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			return adminCredentials{}, "", errors.New("admin configuration must be a regular file")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return adminCredentials{}, "", err
		}
		credentials, err := decodeAdminCredentials(encoded)
		return credentials, "", err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return adminCredentials{}, "", err
	}
	password, err := randomToken(24)
	if err != nil {
		return adminCredentials{}, "", err
	}
	salt := make([]byte, 16)
	if _, err := cryptorand.Read(salt); err != nil {
		return adminCredentials{}, "", err
	}
	digest, err := pbkdf2.Key(sha256.New, password, salt, adminPBKDF2Iterations, sha256.Size)
	if err != nil {
		return adminCredentials{}, "", err
	}
	credentials := adminCredentials{
		Version: 1, Username: "admin", PasswordKDF: "pbkdf2-sha256", Iterations: adminPBKDF2Iterations,
		PasswordSalt: hex.EncodeToString(salt), PasswordHash: hex.EncodeToString(digest),
		passwordSalt: salt, passwordDigest: digest,
	}
	encoded, err = json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return adminCredentials{}, "", err
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		encoded, err = os.ReadFile(path) // #nosec G304 -- the operator selects the credential path.
		if err != nil {
			return adminCredentials{}, "", err
		}
		loaded, err := decodeAdminCredentials(encoded)
		return loaded, "", err
	}
	if err != nil {
		return adminCredentials{}, "", err
	}
	if _, err := file.Write(encoded); err != nil {
		file.Close()
		return adminCredentials{}, "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return adminCredentials{}, "", err
	}
	if err := file.Close(); err != nil {
		return adminCredentials{}, "", err
	}
	return credentials, password, nil
}

func decodeAdminCredentials(encoded []byte) (adminCredentials, error) {
	var credentials adminCredentials
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&credentials); err != nil {
		return credentials, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return credentials, errors.New("invalid trailing admin configuration")
	}
	if credentials.Version != 1 || credentials.Username == "" || credentials.PasswordKDF != "pbkdf2-sha256" ||
		credentials.Iterations < 100_000 || credentials.Iterations > 10_000_000 {
		return credentials, errors.New("invalid admin configuration")
	}
	salt, err := hex.DecodeString(credentials.PasswordSalt)
	if err != nil || len(salt) < 16 {
		return credentials, errors.New("invalid admin password salt")
	}
	digest, err := hex.DecodeString(credentials.PasswordHash)
	if err != nil || len(digest) != sha256.Size {
		return credentials, errors.New("invalid admin password hash")
	}
	credentials.passwordSalt = salt
	credentials.passwordDigest = digest
	return credentials, nil
}

func (server *adminServer) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	adminSecurityHeaders(response)
	response.Header().Set("Cache-Control", "no-store")
	switch {
	case request.URL.Path == "/admin":
		http.Redirect(response, request, "/admin/", http.StatusSeeOther)
	case request.URL.Path == "/admin/login" && request.Method == http.MethodGet:
		if _, ok := server.session(request); ok {
			http.Redirect(response, request, "/admin/", http.StatusSeeOther)
			return
		}
		server.renderLogin(response, http.StatusOK, "")
	case request.URL.Path == "/admin/login" && request.Method == http.MethodPost:
		server.handleLogin(response, request)
	case request.URL.Path == "/admin/logout" && request.Method == http.MethodPost:
		server.handleLogout(response, request)
	case request.URL.Path == "/admin/" && request.Method == http.MethodGet:
		server.handleEditor(response, request)
	case request.URL.Path == "/admin/pools" && request.Method == http.MethodPost:
		server.handleSave(response, request)
	default:
		http.NotFound(response, request)
	}
}

func (server *adminServer) handleLogin(response http.ResponseWriter, request *http.Request) {
	if !requestIsSecure(request) {
		http.Error(response, "HTTPS is required for admin login", http.StatusUpgradeRequired)
		return
	}
	if !sameOrigin(request) {
		http.Error(response, "cross-site request rejected", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 8<<10)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	key := remoteAddress(request)
	if server.loginBlocked(key) {
		response.Header().Set("Retry-After", "300")
		http.Error(response, "too many login attempts; try again later", http.StatusTooManyRequests)
		return
	}
	password := request.PostForm.Get("password")
	digest, err := pbkdf2.Key(sha256.New, password, server.credentials.passwordSalt, server.credentials.Iterations, sha256.Size)
	validPassword := err == nil && subtle.ConstantTimeCompare(digest, server.credentials.passwordDigest) == 1
	validUsername := subtle.ConstantTimeCompare([]byte(request.PostForm.Get("username")), []byte(server.credentials.Username)) == 1
	if !validUsername || !validPassword {
		server.recordLoginFailure(key)
		server.renderLogin(response, http.StatusUnauthorized, "Invalid username or password.")
		return
	}
	server.clearLoginFailures(key)
	token, err := randomToken(32)
	if err != nil {
		http.Error(response, "cannot create session", http.StatusInternalServerError)
		return
	}
	csrf, err := randomToken(32)
	if err != nil {
		http.Error(response, "cannot create session", http.StatusInternalServerError)
		return
	}
	now := server.currentTime()
	server.mu.Lock()
	server.pruneLocked(now)
	server.sessions[token] = adminSession{csrf: csrf, expiresAt: now.Add(adminSessionLifetime)}
	server.mu.Unlock()
	http.SetCookie(response, &http.Cookie{
		Name: adminCookieName, Value: token, Path: "/admin", HttpOnly: true,
		Secure:   requestIsSecure(request),
		SameSite: http.SameSiteStrictMode, MaxAge: int(adminSessionLifetime.Seconds()),
	})
	http.Redirect(response, request, "/admin/", http.StatusSeeOther)
}

func (server *adminServer) handleEditor(response http.ResponseWriter, request *http.Request) {
	session, ok := server.requireSession(response, request)
	if !ok {
		return
	}
	pools, revision, err := server.load()
	if err != nil {
		http.Error(response, "cannot load pool registry", http.StatusInternalServerError)
		return
	}
	page := adminEditorPage{CSRFToken: session.csrf, PoolsJSON: string(pools), Revision: revision}
	if request.URL.Query().Get("saved") == "1" {
		page.Message = "Pool registry saved and activated. Scouts will receive the new revision on their next config fetch."
	}
	server.renderEditor(response, http.StatusOK, page)
}

func (server *adminServer) handleSave(response http.ResponseWriter, request *http.Request) {
	session, ok := server.requireSession(response, request)
	if !ok {
		return
	}
	if !sameOrigin(request) {
		http.Error(response, "cross-site request rejected", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 2<<20)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.PostForm.Get("csrf")), []byte(session.csrf)) != 1 {
		http.Error(response, "invalid CSRF token", http.StatusForbidden)
		return
	}
	raw := []byte(request.PostForm.Get("pools_json"))
	revision, err := server.save(raw)
	if err != nil {
		server.renderEditor(response, http.StatusUnprocessableEntity, adminEditorPage{
			CSRFToken: session.csrf, PoolsJSON: string(raw), Error: err.Error(),
		})
		return
	}
	target := "/admin/?saved=1&revision=" + url.QueryEscape(revision)
	http.Redirect(response, request, target, http.StatusSeeOther)
}

func (server *adminServer) handleLogout(response http.ResponseWriter, request *http.Request) {
	session, ok := server.requireSession(response, request)
	if !ok {
		return
	}
	if !sameOrigin(request) {
		http.Error(response, "cross-site request rejected", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 8<<10)
	if err := request.ParseForm(); err != nil || subtle.ConstantTimeCompare([]byte(request.PostForm.Get("csrf")), []byte(session.csrf)) != 1 {
		http.Error(response, "invalid CSRF token", http.StatusForbidden)
		return
	}
	if cookie, err := request.Cookie(adminCookieName); err == nil {
		server.mu.Lock()
		delete(server.sessions, cookie.Value)
		server.mu.Unlock()
	}
	http.SetCookie(response, &http.Cookie{Name: adminCookieName, Path: "/admin", MaxAge: -1, HttpOnly: true, Secure: cookieIsSecure(request), SameSite: http.SameSiteStrictMode})
	http.Redirect(response, request, "/admin/login", http.StatusSeeOther)
}

func (server *adminServer) requireSession(response http.ResponseWriter, request *http.Request) (adminSession, bool) {
	if !requestIsSecure(request) {
		http.Error(response, "HTTPS is required for admin access", http.StatusUpgradeRequired)
		return adminSession{}, false
	}
	session, ok := server.session(request)
	if !ok {
		http.Redirect(response, request, "/admin/login", http.StatusSeeOther)
	}
	return session, ok
}

func (server *adminServer) session(request *http.Request) (adminSession, bool) {
	cookie, err := request.Cookie(adminCookieName)
	if err != nil {
		return adminSession{}, false
	}
	now := server.currentTime()
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneLocked(now)
	session, ok := server.sessions[cookie.Value]
	return session, ok && session.expiresAt.After(now)
}

func (server *adminServer) loginBlocked(key string) bool {
	now := server.currentTime()
	server.mu.Lock()
	defer server.mu.Unlock()
	failure := server.failures[key]
	return now.Sub(failure.windowStart) < adminFailureWindow && failure.count >= adminMaxLoginFailures
}

func (server *adminServer) recordLoginFailure(key string) {
	now := server.currentTime()
	server.mu.Lock()
	defer server.mu.Unlock()
	failure := server.failures[key]
	if failure.windowStart.IsZero() || now.Sub(failure.windowStart) >= adminFailureWindow {
		failure = loginFailures{windowStart: now}
	}
	failure.count++
	server.failures[key] = failure
}

func (server *adminServer) clearLoginFailures(key string) {
	server.mu.Lock()
	delete(server.failures, key)
	server.mu.Unlock()
}

func (server *adminServer) pruneLocked(now time.Time) {
	for token, session := range server.sessions {
		if !session.expiresAt.After(now) {
			delete(server.sessions, token)
		}
	}
	for key, failure := range server.failures {
		if now.Sub(failure.windowStart) >= adminFailureWindow {
			delete(server.failures, key)
		}
	}
}

func (server *adminServer) currentTime() time.Time {
	if server.now != nil {
		return server.now().UTC()
	}
	return time.Now().UTC()
}

func (server *adminServer) renderLogin(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = server.login.Execute(response, struct{ Error string }{Error: message})
}

func (server *adminServer) renderEditor(response http.ResponseWriter, status int, page adminEditorPage) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = server.editor.Execute(response, page)
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := cryptorand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func remoteAddress(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	if address := net.ParseIP(host); address != nil && address.IsLoopback() {
		if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
			return forwarded
		}
	}
	return host
}

func cookieIsSecure(request *http.Request) bool {
	return request.TLS != nil || strings.EqualFold(request.Header.Get("X-Forwarded-Proto"), "https")
}

func requestIsSecure(request *http.Request) bool {
	if forwarded := request.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		return strings.EqualFold(forwarded, "https")
	}
	if request.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func sameOrigin(request *http.Request) bool {
	if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == request.Host
}

func adminSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	response.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Frame-Options", "DENY")
}

const adminLoginHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Admin login — StratumStats</title><link rel="stylesheet" href="/static/style.css?v=381197db"></head>
<body><main class="admin-shell"><section class="admin-card"><a class="brand" href="/"><span>StratumStats</span></a><p class="eyebrow">Administration</p><h1>Pool registry login</h1>{{if .Error}}<p class="admin-error" role="alert">{{.Error}}</p>{{end}}<form method="post" action="/admin/login" class="admin-form"><label>Username<input name="username" autocomplete="username" required autofocus></label><label>Password<input type="password" name="password" autocomplete="current-password" required></label><button type="submit">Sign in</button></form></section></main></body></html>`

const adminEditorHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Pool registry — StratumStats</title><link rel="stylesheet" href="/static/style.css?v=381197db"></head>
<body><header><a class="brand" href="/">StratumStats</a><form method="post" action="/admin/logout"><input type="hidden" name="csrf" value="{{.CSRFToken}}"><button type="submit" class="admin-link-button">Sign out</button></form></header><main class="admin-editor"><div class="admin-editor-head"><div><p class="eyebrow">Administration</p><h1>Edit pool registry</h1><p>Saving validates the complete registry, writes it atomically, and activates it immediately. External file edits require <code>SIGUSR1</code>.</p></div><code class="admin-revision">{{.Revision}}</code></div>{{if .Message}}<p class="admin-success" role="status">{{.Message}}</p>{{end}}{{if .Error}}<p class="admin-error" role="alert">{{.Error}}</p>{{end}}<form method="post" action="/admin/pools" class="admin-json-form"><input type="hidden" name="csrf" value="{{.CSRFToken}}"><label for="pools-json">config/pools.json</label><textarea id="pools-json" name="pools_json" spellcheck="false" required>{{.PoolsJSON}}</textarea><div class="admin-actions"><button type="submit">Validate and save</button><a href="/admin/">Discard changes</a></div></form></main></body></html>`

func defaultAdminPath(dataPath string) string {
	return filepath.Join(filepath.Dir(dataPath), "admin.json")
}
