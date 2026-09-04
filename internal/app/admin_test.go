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
	if got := loginResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("login Cache-Control=%q", got)
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

func TestSameOriginBehindTrustedLoopbackProxy(t *testing.T) {
	newRequest := func(host, origin, fetchSite, forwardedProto string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, "http://"+host+"/admin/login", nil)
		request.RemoteAddr = "127.0.0.1:45678"
		request.Header.Set("Origin", origin)
		request.Header.Set("Sec-Fetch-Site", fetchSite)
		request.Header.Set("X-Forwarded-Proto", forwardedProto)
		return request
	}

	for _, test := range []struct {
		name   string
		host   string
		origin string
		fetch  string
		proto  string
		want   bool
	}{
		{name: "public HTTPS accepts inconsistent Fetch Metadata", host: "stratumstats.m45core.com", origin: "https://stratumstats.m45core.com", fetch: "cross-site", proto: "https", want: true},
		{name: "default HTTPS port matches explicit origin port", host: "stratumstats.m45core.com", origin: "https://stratumstats.m45core.com:443", fetch: "same-origin", proto: "https", want: true},
		{name: "default HTTPS port matches explicit request port", host: "stratumstats.m45core.com:443", origin: "https://stratumstats.m45core.com", fetch: "same-origin", proto: "https", want: true},
		{name: "scheme mismatch is rejected", host: "stratumstats.m45core.com", origin: "http://stratumstats.m45core.com", fetch: "same-origin", proto: "https", want: false},
		{name: "cross-site origin is rejected", host: "stratumstats.m45core.com", origin: "https://attacker.example", fetch: "same-origin", proto: "https", want: false},
		{name: "malformed origin is rejected", host: "stratumstats.m45core.com", origin: "https://stratumstats.m45core.com/path", fetch: "same-origin", proto: "https", want: false},
		{name: "unsupported origin scheme is rejected", host: "stratumstats.m45core.com", origin: "file://stratumstats.m45core.com", fetch: "same-origin", proto: "https", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sameOrigin(newRequest(test.host, test.origin, test.fetch, test.proto)); got != test.want {
				t.Fatalf("sameOrigin()=%v, want %v", got, test.want)
			}
		})
	}
}

func TestForwardedProtoRequiresTrustedLoopbackProxy(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://stratumstats.m45core.com/admin/login", nil)
	request.RemoteAddr = "198.51.100.10:45678"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://stratumstats.m45core.com")
	if requestIsSecure(request) {
		t.Fatal("untrusted X-Forwarded-Proto made request secure")
	}
	if sameOrigin(request) {
		t.Fatal("untrusted X-Forwarded-Proto made HTTPS origin match HTTP request")
	}
}

func TestAdminLoginBehindTrustedLoopbackProxyAcceptsPublicHTTPSOrigin(t *testing.T) {
	handler, _, err := newAdminHandler(filepath.Join(t.TempDir(), "admin.json"),
		func() ([]byte, string, error) { return []byte(`{"pools":[]}`), "sha256:current", nil },
		func([]byte) (string, error) { return "", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://stratumstats.m45core.com/admin/login", strings.NewReader("username=admin&password=wrong"))
	request.RemoteAddr = "127.0.0.1:45678"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Host", "stratumstats.m45core.com")
	request.Header.Set("Origin", "https://stratumstats.m45core.com")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
}
