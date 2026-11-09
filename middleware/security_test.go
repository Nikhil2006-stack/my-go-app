package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("MyStr0ngP@ssw0rd!")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !strings.HasPrefix(hash, "pbkdf2$") {
		t.Fatalf("expected pbkdf2$ prefix, got %q", hash)
	}
	if !VerifyPassword("MyStr0ngP@ssw0rd!", hash) {
		t.Fatalf("expected VerifyPassword to succeed for correct password")
	}
	if VerifyPassword("WrongPassword", hash) {
		t.Fatalf("expected VerifyPassword to fail for wrong password")
	}
}

func TestAuthSessionAndCSRF(t *testing.T) {
	t.Setenv("ADMIN_USER", "nikhil_admin")
	t.Setenv("ADMIN_PASSWORD", "SuperSecret123!")
	t.Setenv("ADMIN_PASSWORD_HASH", "")
	t.Setenv("SESSION_SECRET", "test-secret-key-for-hmac-signing-12345")

	auth := NewAuthManager()
	if !auth.IsConfigured() {
		t.Fatalf("expected AuthManager to be configured")
	}

	if !auth.Authenticate("nikhil_admin", "SuperSecret123!") {
		t.Fatalf("expected valid credentials to authenticate")
	}
	if auth.Authenticate("nikhil_admin", "wrong") {
		t.Fatalf("expected wrong password to fail authentication")
	}
	if auth.Authenticate("wrong_user", "SuperSecret123!") {
		t.Fatalf("expected wrong username to fail authentication")
	}

	// Test RequireAuth redirects unauthenticated requests to /admin/login
	protected := auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	unauthReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	unauthRec := httptest.NewRecorder()
	protected(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusSeeOther || unauthRec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("expected redirect to /admin/login, got status=%d loc=%q", unauthRec.Code, unauthRec.Header().Get("Location"))
	}

	// Create session
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	loginReq.Header.Set("X-Forwarded-Proto", "https")
	loginRec := httptest.NewRecorder()
	sess, err := auth.CreateSession(loginRec, loginReq, "nikhil_admin")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected session cookie to be set")
	}
	sessionCookie := cookies[0]
	if !sessionCookie.HttpOnly || !sessionCookie.Secure {
		t.Fatalf("expected HttpOnly and Secure cookie flags, got HttpOnly=%v Secure=%v", sessionCookie.HttpOnly, sessionCookie.Secure)
	}

	// Authenticated request passes RequireAuth
	authReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	authReq.AddCookie(sessionCookie)
	authRec := httptest.NewRecorder()
	protected(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for authenticated session, got %d", authRec.Code)
	}

	// Test CSRF validation
	form := url.Values{}
	form.Set("csrf_token", sess.CSRFToken)
	csrfReq := httptest.NewRequest(http.MethodPost, "/admin", strings.NewReader(form.Encode()))
	csrfReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	csrfReq.AddCookie(sessionCookie)
	if !auth.VerifySessionCSRF(csrfReq) {
		t.Fatalf("expected valid session CSRF token to pass")
	}

	// Logout destroys session
	logoutRec := httptest.NewRecorder()
	auth.DestroySession(logoutRec, authReq)
	if _, ok := auth.GetSession(authReq); ok {
		t.Fatalf("expected session to be invalidated after DestroySession")
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	ip := "203.0.113.10"

	for i := 0; i < 3; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("expected request %d to be allowed", i+1)
		}
	}
	if rl.Allow(ip) {
		t.Fatalf("expected 4th request to be rate-limited")
	}
	if !rl.Allow("203.0.113.99") {
		t.Fatalf("expected different IP to be allowed")
	}
}
