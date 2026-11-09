package middleware

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SessionCookieName = "portfolio_admin_session"
	CSRFCookieName    = "portfolio_csrf_token"
	SessionTTL        = 12 * time.Hour
	pbkdf2Iterations  = 120000
	pbkdf2KeyLen      = 32
)

// Session represents an active authenticated administrator session.
type Session struct {
	Token     string
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

// AuthManager manages administrator credentials, password hashing, sessions, and CSRF protection.
type AuthManager struct {
	mu           sync.RWMutex
	adminUser    string
	passwordHash string // formatted as pbkdf2$iterations$saltHex$dkHex
	secretKey    []byte
	sessions     map[string]Session
}

// NewAuthManager initializes AuthManager from environment variables.
// Sensitive credentials are read exclusively from environment variables (never hardcoded).
func NewAuthManager() *AuthManager {
	user := strings.TrimSpace(os.Getenv("ADMIN_USER"))
	rawPass := os.Getenv("ADMIN_PASSWORD")
	passHash := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD_HASH"))
	secret := os.Getenv("SESSION_SECRET")

	var secretBytes []byte
	if secret != "" {
		sum := sha256.Sum256([]byte(secret))
		secretBytes = sum[:]
	} else {
		secretBytes = make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			log.Println("WARNING: failed to generate random session secret:", err)
		}
	}

	if passHash == "" && rawPass != "" {
		hashed, err := HashPassword(rawPass)
		if err != nil {
			log.Println("ERROR: failed to hash ADMIN_PASSWORD:", err)
		} else {
			passHash = hashed
		}
	}

	return &AuthManager{
		adminUser:    user,
		passwordHash: passHash,
		secretKey:    secretBytes,
		sessions:     make(map[string]Session),
	}
}

// IsConfigured reports whether administrator credentials have been configured in the environment.
func (a *AuthManager) IsConfigured() bool {
	return a.adminUser != "" && a.passwordHash != ""
}

// HashPassword hashes a plaintext password using PBKDF2-HMAC-SHA256 with a random 16-byte salt.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password cannot be empty")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s",
		pbkdf2Iterations,
		hex.EncodeToString(salt),
		hex.EncodeToString(dk),
	), nil
}

// VerifyPassword compares a plaintext password against a PBKDF2 hash in constant time.
func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1000 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	expectedDK, err := hex.DecodeString(parts[3])
	if err != nil || len(expectedDK) == 0 {
		return false
	}

	actualDK, err := pbkdf2.Key(sha256.New, password, salt, iter, len(expectedDK))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actualDK, expectedDK) == 1
}

// Authenticate verifies username and password in constant time.
func (a *AuthManager) Authenticate(username, password string) bool {
	if !a.IsConfigured() {
		return false
	}
	userMatch := subtle.ConstantTimeCompare([]byte(username), []byte(a.adminUser)) == 1
	passMatch := VerifyPassword(password, a.passwordHash)
	return userMatch && passMatch
}

// CreateSession generates a new signed session token and CSRF token for an authenticated admin.
func (a *AuthManager) CreateSession(w http.ResponseWriter, r *http.Request, username string) (Session, error) {
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return Session{}, err
	}
	csrfRaw := make([]byte, 32)
	if _, err := rand.Read(csrfRaw); err != nil {
		return Session{}, err
	}

	tokenID := base64.RawURLEncoding.EncodeToString(rawToken)
	signedToken := a.signValue(tokenID)
	csrfToken := base64.RawURLEncoding.EncodeToString(csrfRaw)
	expiresAt := time.Now().Add(SessionTTL)

	sess := Session{
		Token:     signedToken,
		Username:  username,
		CSRFToken: csrfToken,
		ExpiresAt: expiresAt,
	}

	a.mu.Lock()
	a.cleanupExpiredLocked()
	a.sessions[signedToken] = sess
	a.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    signedToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(SessionTTL.Seconds()),
	})

	return sess, nil
}

// GetSession retrieves and validates the current admin session from the request cookie.
func (a *AuthManager) GetSession(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}

	if !a.verifySignedValue(cookie.Value) {
		return Session{}, false
	}

	a.mu.RLock()
	sess, exists := a.sessions[cookie.Value]
	a.mu.RUnlock()

	if !exists || time.Now().After(sess.ExpiresAt) {
		if exists {
			a.mu.Lock()
			delete(a.sessions, cookie.Value)
			a.mu.Unlock()
		}
		return Session{}, false
	}

	return sess, true
}

// DestroySession invalidates the session on the server and clears the browser cookie.
func (a *AuthManager) DestroySession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// RequireAuth is middleware that protects admin routes and redirects unauthenticated users to /admin/login.
func (a *AuthManager) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.GetSession(r); !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
		next(w, r)
	}
}

// VerifySessionCSRF validates that the submitted csrf_token matches the active admin session's CSRF token.
func (a *AuthManager) VerifySessionCSRF(r *http.Request) bool {
	sess, ok := a.GetSession(r)
	if !ok {
		return false
	}
	submitted := strings.TrimSpace(r.FormValue("csrf_token"))
	if submitted == "" {
		submitted = strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	}
	if submitted == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(sess.CSRFToken)) == 1
}

// EnsurePublicCSRFToken issues or reuses a signed CSRF token cookie for public forms (such as the contact form).
func (a *AuthManager) EnsurePublicCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(CSRFCookieName); err == nil && a.verifySignedValue(cookie.Value) {
		return cookie.Value
	}

	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	signed := a.signValue(base64.RawURLEncoding.EncodeToString(raw))

	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((4 * time.Hour).Seconds()),
	})
	return signed
}

// VerifyPublicCSRF checks that the public form's csrf_token matches the signed cookie token.
func (a *AuthManager) VerifyPublicCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(CSRFCookieName)
	if err != nil || !a.verifySignedValue(cookie.Value) {
		return false
	}
	submitted := strings.TrimSpace(r.FormValue("csrf_token"))
	if submitted == "" {
		submitted = strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	}
	if submitted == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(cookie.Value)) == 1
}

func (a *AuthManager) signValue(payload string) string {
	mac := hmac.New(sha256.New, a.secretKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

func (a *AuthManager) verifySignedValue(signed string) bool {
	idx := strings.LastIndexByte(signed, '.')
	if idx <= 0 {
		return false
	}
	payload := signed[:idx]
	expected := a.signValue(payload)
	return subtle.ConstantTimeCompare([]byte(signed), []byte(expected)) == 1
}

func (a *AuthManager) cleanupExpiredLocked() {
	now := time.Now()
	for k, v := range a.sessions {
		if now.After(v.ExpiresAt) {
			delete(a.sessions, k)
		}
	}
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
