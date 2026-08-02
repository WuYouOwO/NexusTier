package auth

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testPassword = "correct horse battery staple"

func testGuard(t *testing.T) *Guard {
	t.Helper()
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	credential, err := NewCredential("operator", hash)
	if err != nil {
		t.Fatalf("NewCredential() error = %v", err)
	}
	signer, err := NewSessionSigner(nil, time.Hour)
	if err != nil {
		t.Fatalf("NewSessionSigner() error = %v", err)
	}
	guard, err := NewGuard(credential, signer, slog.New(slog.NewTextHandler(io.Discard, nil)), true)
	if err != nil {
		t.Fatalf("NewGuard() error = %v", err)
	}
	return guard
}

func protectedHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("secret payload"))
	})
}

func TestHashRoundTripAcceptsOnlyTheRightPair(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	credential, err := NewCredential("operator", hash)
	if err != nil {
		t.Fatalf("NewCredential() error = %v", err)
	}
	if !credential.Verify("operator", testPassword) {
		t.Fatal("Verify() rejected the correct pair")
	}
	if credential.Verify("operator", testPassword+"x") {
		t.Fatal("Verify() accepted a wrong password")
	}
	if credential.Verify("intruder", testPassword) {
		t.Fatal("Verify() accepted a wrong username")
	}
}

func TestHashIsSaltedPerCall(t *testing.T) {
	first, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	second, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if first == second {
		t.Fatal("identical hashes for the same password: salt is not random")
	}
}

func TestNewCredentialRejectsWeakOrBrokenHashes(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"wrong scheme", "bcrypt$10$abc$def"},
		{"too few fields", "pbkdf2-sha256$600000$abc"},
		{"iterations below floor", "pbkdf2-sha256$1000$YWJjZGVmZ2hpamtsbW5vcA$YWJjZGVmZ2hpamtsbW5vcA"},
		{"salt not base64", "pbkdf2-sha256$600000$!!!$YWJjZGVmZ2hpamtsbW5vcA"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewCredential("operator", test.hash); err == nil {
				t.Fatal("NewCredential() accepted an unusable hash")
			}
		})
	}
}

func TestSessionRoundTripReturnsTheSubject(t *testing.T) {
	signer, err := NewSessionSigner(nil, time.Hour)
	if err != nil {
		t.Fatalf("NewSessionSigner() error = %v", err)
	}
	now := time.Now()
	token, err := signer.Issue("operator", now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	subject, err := signer.Verify(token, now)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if subject != "operator" {
		t.Fatalf("subject = %q, want operator", subject)
	}
}

func TestSessionRejectsForgedAndExpiredTokens(t *testing.T) {
	signer, err := NewSessionSigner(nil, time.Hour)
	if err != nil {
		t.Fatalf("NewSessionSigner() error = %v", err)
	}
	now := time.Now()
	token, err := signer.Issue("operator", now)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	if _, err := signer.Verify(token+"x", now); err == nil {
		t.Fatal("Verify() accepted a tampered signature")
	}
	if _, err := signer.Verify(token, now.Add(2*time.Hour)); err == nil {
		t.Fatal("Verify() accepted an expired token")
	}
	other, err := NewSessionSigner(nil, time.Hour)
	if err != nil {
		t.Fatalf("NewSessionSigner() error = %v", err)
	}
	if _, err := other.Verify(token, now); err == nil {
		t.Fatal("Verify() accepted a token signed with a different key")
	}
}

func TestSessionSignerRejectsShortSuppliedKeys(t *testing.T) {
	if _, err := NewSessionSigner([]byte("too short"), time.Hour); err == nil {
		t.Fatal("NewSessionSigner() accepted a key below the minimum length")
	}
}

func TestLimiterThrottlesAfterTheBurstAndRefundsSuccess(t *testing.T) {
	limiter := NewLoginLimiter()
	frozen := time.Now()
	limiter.now = func() time.Time { return frozen }

	for attempt := 0; attempt < defaultBurst; attempt++ {
		if !limiter.Allow("10.0.0.1") {
			t.Fatalf("attempt %d denied inside the burst budget", attempt+1)
		}
	}
	if limiter.Allow("10.0.0.1") {
		t.Fatal("Allow() did not throttle once the budget was spent")
	}
	if !limiter.Allow("10.0.0.2") {
		t.Fatal("throttling one client must not affect another")
	}

	limiter.Refund("10.0.0.1")
	if !limiter.Allow("10.0.0.1") {
		t.Fatal("Allow() denied a client after its token was refunded")
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	limiter := NewLoginLimiter()
	frozen := time.Now()
	limiter.now = func() time.Time { return frozen }
	for attempt := 0; attempt < defaultBurst; attempt++ {
		limiter.Allow("10.0.0.1")
	}
	if limiter.Allow("10.0.0.1") {
		t.Fatal("Allow() did not throttle once the budget was spent")
	}

	frozen = frozen.Add(defaultRefill)
	if !limiter.Allow("10.0.0.1") {
		t.Fatal("Allow() did not replenish a token after the refill window")
	}
}

func TestProtectRejectsAnonymousAPICallsWithJSON(t *testing.T) {
	handler := testGuard(t).Protect(protectedHandler())
	request := httptest.NewRequest(http.MethodGet, "/v1/topology", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if !strings.Contains(response.Body.String(), "unauthenticated") {
		t.Fatalf("body = %q, want an unauthenticated envelope", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret payload") {
		t.Fatal("anonymous caller reached the protected handler")
	}
}

func TestProtectRedirectsAnonymousBrowsersToLogin(t *testing.T) {
	handler := testGuard(t).Protect(protectedHandler())
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if location := response.Header().Get("Location"); location != LoginPath {
		t.Fatalf("Location = %q, want %s", location, LoginPath)
	}
}

func TestProtectKeepsProbesPublic(t *testing.T) {
	handler := testGuard(t).Protect(protectedHandler())
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
		})
	}
}

func TestLoginIssuesAHardenedCookieAndAdmitsTheSession(t *testing.T) {
	handler := testGuard(t).Protect(protectedHandler())
	form := url.Values{"username": {"operator"}, "password": {testPassword}}
	request := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	cookies := response.Result().Cookies()
	var session *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == SessionCookie {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("login did not set a session cookie")
	}
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie is not hardened: %+v", session)
	}

	authenticated := httptest.NewRequest(http.MethodGet, "/v1/topology", nil)
	authenticated.AddCookie(session)
	authorized := httptest.NewRecorder()

	handler.ServeHTTP(authorized, authenticated)

	if authorized.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an authenticated caller", authorized.Code)
	}
}

func TestLoginRejectsWrongCredentialsWithoutEchoingThem(t *testing.T) {
	handler := testGuard(t).Protect(protectedHandler())
	form := url.Values{"username": {"operator"}, "password": {"wrong-secret-value"}}
	request := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("a failed login issued a cookie")
	}
	if strings.Contains(response.Body.String(), "wrong-secret-value") {
		t.Fatal("login page echoed the submitted password")
	}
}

func TestLoginThrottlesRepeatedFailures(t *testing.T) {
	guard := testGuard(t)
	handler := guard.Protect(protectedHandler())
	form := url.Values{"username": {"operator"}, "password": {"wrong"}}

	var last *httptest.ResponseRecorder
	for attempt := 0; attempt < defaultBurst+1; attempt++ {
		request := httptest.NewRequest(http.MethodPost, LoginPath, strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.RemoteAddr = "10.9.9.9:5555"
		last = httptest.NewRecorder()
		handler.ServeHTTP(last, request)
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after exhausting the budget", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatal("throttled response omitted Retry-After")
	}
}

func TestLogoutClearsTheCookie(t *testing.T) {
	handler := testGuard(t).Protect(protectedHandler())
	request := httptest.NewRequest(http.MethodPost, LogoutPath, nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == SessionCookie {
			if cookie.MaxAge >= 0 || cookie.Value != "" {
				t.Fatalf("logout did not expire the cookie: %+v", cookie)
			}
			return
		}
	}
	t.Fatal("logout did not touch the session cookie")
}

func TestLoginFormRendersWithoutASession(t *testing.T) {
	handler := testGuard(t).Protect(protectedHandler())
	request := httptest.NewRequest(http.MethodGet, LoginPath, nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	if !strings.Contains(body, `name="username"`) || !strings.Contains(body, `name="password"`) {
		t.Fatal("login page is missing its credential fields")
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("login page is missing a Content-Security-Policy header")
	}
}
