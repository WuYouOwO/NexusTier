package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// SessionCookie names the cookie carrying the signed session token.
	SessionCookie = "nexustier_session"
	// LoginPath serves the console login form.
	LoginPath = "/login"
	// LogoutPath ends the current session.
	LogoutPath = "/logout"
)

// Guard authenticates every request that is not explicitly public.
type Guard struct {
	credential   Credential
	signer       *SessionSigner
	limiter      *LoginLimiter
	logger       *slog.Logger
	secureCookie bool
	now          func() time.Time
}

// NewGuard builds a guard for the supplied operator credential. Set
// secureCookie to false only when the console is reached over plain HTTP on a
// non-loopback address, because browsers drop Secure cookies on such origins.
func NewGuard(credential Credential, signer *SessionSigner, logger *slog.Logger, secureCookie bool) (*Guard, error) {
	if signer == nil {
		return nil, errors.New("session signer is required")
	}
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	return &Guard{
		credential:   credential,
		signer:       signer,
		limiter:      NewLoginLimiter(),
		logger:       logger,
		secureCookie: secureCookie,
		now:          time.Now,
	}, nil
}

// publicPaths stay reachable without a session. Probes must answer before an
// operator has logged in, and the login form obviously cannot require one. The
// console bundle stays private: the login page is self-contained HTML.
var publicPaths = map[string]struct{}{
	"/healthz": {},
	"/readyz":  {},
	LoginPath:  {},
	LogoutPath: {},
}

// Protect wraps the application handler, admitting only authenticated callers.
func (guard *Guard) Protect(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+LoginPath, guard.loginForm)
	mux.HandleFunc("POST "+LoginPath, guard.login)
	mux.HandleFunc("POST "+LogoutPath, guard.logout)
	mux.Handle("/", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if _, public := publicPaths[request.URL.Path]; public {
			next.ServeHTTP(writer, request)
			return
		}
		if guard.subject(request) == "" {
			guard.reject(writer, request)
			return
		}
		next.ServeHTTP(writer, request)
	}))
	return mux
}

// subject returns the authenticated username, or an empty string.
func (guard *Guard) subject(request *http.Request) string {
	cookie, err := request.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		return ""
	}
	subject, err := guard.signer.Verify(cookie.Value, guard.now())
	if err != nil {
		return ""
	}
	return subject
}

// reject answers an unauthenticated request: a redirect for a browser
// navigation, a JSON envelope for an API client.
func (guard *Guard) reject(writer http.ResponseWriter, request *http.Request) {
	if wantsHTML(request) {
		http.Redirect(writer, request, LoginPath, http.StatusSeeOther)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"error": map[string]string{
			"code":    "unauthenticated",
			"message": "a valid session is required",
		},
	})
}

func (guard *Guard) loginForm(writer http.ResponseWriter, request *http.Request) {
	if guard.subject(request) != "" {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}
	guard.renderLogin(writer, http.StatusOK, "")
}

func (guard *Guard) login(writer http.ResponseWriter, request *http.Request) {
	client := clientAddress(request)
	if !guard.limiter.Allow(client) {
		guard.logger.Warn("login throttled", "client", client)
		writer.Header().Set("Retry-After", retryAfterSeconds(guard.limiter.RetryAfter()))
		guard.renderLogin(writer, http.StatusTooManyRequests, "尝试过于频繁，请稍后再试。")
		return
	}
	if err := request.ParseForm(); err != nil {
		guard.renderLogin(writer, http.StatusBadRequest, "请求格式无效。")
		return
	}
	username := request.PostFormValue("username")
	password := request.PostFormValue("password")
	if !guard.credential.Verify(username, password) {
		// Log the source but never the submitted credentials.
		guard.logger.Warn("login rejected", "client", client)
		guard.renderLogin(writer, http.StatusUnauthorized, "用户名或密码错误。")
		return
	}
	token, err := guard.signer.Issue(guard.credential.Username, guard.now())
	if err != nil {
		guard.logger.Error("issue session failed", "error", err)
		guard.renderLogin(writer, http.StatusInternalServerError, "无法建立会话，请重试。")
		return
	}
	guard.limiter.Refund(client)
	http.SetCookie(writer, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   guard.secureCookie,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(guard.signer.TTL().Seconds()),
	})
	guard.logger.Info("login accepted", "client", client, "subject", guard.credential.Username)
	http.Redirect(writer, request, "/", http.StatusSeeOther)
}

func (guard *Guard) logout(writer http.ResponseWriter, request *http.Request) {
	http.SetCookie(writer, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   guard.secureCookie,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.Redirect(writer, request, LoginPath, http.StatusSeeOther)
}

func wantsHTML(request *http.Request) bool {
	return strings.Contains(request.Header.Get("Accept"), "text/html")
}

func retryAfterSeconds(duration time.Duration) string {
	seconds := int(duration.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
