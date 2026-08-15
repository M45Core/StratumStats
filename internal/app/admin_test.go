package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminCredentialsAndAuthenticatedRegistrySave(t *testing.T) {
	credentialPath := filepath.Join(t.TempDir(), "state", "admin.json")
	const registry = `{"pools":[{"id":"pool","name":"Pool","endpoints":[{"host":"pool.example","port":3333,"tls":false}]}]}`
	var saved string
	handler, password, err := newAdminHandler(credentialPath,
		func() ([]byte, string, error) { return []byte(registry), "sha256:current", nil },
		func(raw []byte) (string, error) { saved = string(raw); return "sha256:new", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if password == "" {
		t.Fatal("initial password was not generated")
	}
	credentials, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(credentials), password) {
		t.Fatal("plaintext password was stored")
	}
	info, err := os.Stat(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode=%o", info.Mode().Perm())
	}

	insecure := httptest.NewRequest(http.MethodPost, "http://stats.example/admin/login", nil)
	insecureResponse := httptest.NewRecorder()
	handler.ServeHTTP(insecureResponse, insecure)
	if insecureResponse.Code != http.StatusUpgradeRequired {
		t.Fatalf("insecure login status=%d", insecureResponse.Code)
	}

	loginForm := url.Values{"username": {"admin"}, "password": {password}}
	login := httptest.NewRequest(http.MethodPost, "https://stats.example/admin/login", strings.NewReader(loginForm.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.Header.Set("Origin", "https://stats.example")
	login.Header.Set("X-Forwarded-Proto", "https")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	cookies := loginResponse.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie=%+v", cookies)
	}

	server := handler.(*adminServer)
	server.mu.Lock()
	session := server.sessions[cookies[0].Value]
	server.mu.Unlock()
	if session.csrf == "" {
		t.Fatal("missing CSRF token")
	}
	updated := strings.Replace(registry, "Pool", "Updated Pool", 1)
	saveForm := url.Values{"csrf": {session.csrf}, "pools_json": {updated}}
	saveRequest := httptest.NewRequest(http.MethodPost, "https://stats.example/admin/pools", strings.NewReader(saveForm.Encode()))
	saveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveRequest.Header.Set("Origin", "https://stats.example")
	saveRequest.Header.Set("X-Forwarded-Proto", "https")
	saveRequest.AddCookie(cookies[0])
	saveResponse := httptest.NewRecorder()
	handler.ServeHTTP(saveResponse, saveRequest)
	if saveResponse.Code != http.StatusSeeOther || saved != updated {
		body, _ := io.ReadAll(saveResponse.Result().Body)
		t.Fatalf("save status=%d saved=%q body=%s", saveResponse.Code, saved, body)
	}
}
