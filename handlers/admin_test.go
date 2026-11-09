package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"my-go-app/database"
	"my-go-app/middleware"
)

func TestValidateProjectInput(t *testing.T) {
	p, err := ValidateProjectInput(
		"Portfolio Website",
		"A full-stack portfolio built with Go and PostgreSQL.",
		"Go, PostgreSQL, HTML, CSS",
		"https://example.com",
		"https://github.com/Nikhil2006-stack/my-go-app",
	)
	if err != nil {
		t.Fatalf("expected valid project input to pass, got %v", err)
	}
	if p.Title != "Portfolio Website" {
		t.Fatalf("unexpected title: %q", p.Title)
	}

	// Reject javascript: URLs (XSS prevention)
	_, err = ValidateProjectInput(
		"Bad Project",
		"Valid description here for testing.",
		"Go",
		"javascript:alert(1)",
		"",
	)
	if err == nil {
		t.Fatalf("expected javascript: URL to be rejected")
	}

	// Reject too-short title
	_, err = ValidateProjectInput("A", "Valid description here for testing.", "Go", "", "")
	if err == nil {
		t.Fatalf("expected 1-char title to be rejected")
	}
}

func TestAdminLoginAndProjectCRUD(t *testing.T) {
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "StrongAdminPass123!")
	t.Setenv("SESSION_SECRET", "test-session-secret-key-987654321")

	store := database.NewFallbackStore()
	auth := middleware.NewAuthManager()
	srv := NewServer(store, auth, filepath.Join("..", "templates"))

	// 1. GET /admin/login to obtain CSRF cookie
	getLoginReq := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	getLoginRec := httptest.NewRecorder()
	srv.AdminLoginHandler(getLoginRec, getLoginReq)
	if getLoginRec.Code != http.StatusOK {
		t.Fatalf("GET /admin/login expected 200, got %d", getLoginRec.Code)
	}
	var csrfCookie *http.Cookie
	for _, c := range getLoginRec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatalf("expected CSRF cookie from GET /admin/login")
	}

	// 2. POST /admin/login with valid credentials
	loginForm := url.Values{}
	loginForm.Set("username", "admin")
	loginForm.Set("password", "StrongAdminPass123!")
	loginForm.Set("csrf_token", csrfCookie.Value)

	postLoginReq := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(loginForm.Encode()))
	postLoginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postLoginReq.AddCookie(csrfCookie)
	postLoginRec := httptest.NewRecorder()
	srv.AdminLoginHandler(postLoginRec, postLoginReq)

	if postLoginRec.Code != http.StatusSeeOther || postLoginRec.Header().Get("Location") != "/admin" {
		t.Fatalf("expected redirect to /admin after login, got %d (%s)", postLoginRec.Code, postLoginRec.Header().Get("Location"))
	}

	var sessionCookie *http.Cookie
	for _, c := range postLoginRec.Result().Cookies() {
		if c.Name == middleware.SessionCookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected session cookie after successful login")
	}

	// Obtain session CSRF token
	authedReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	authedReq.AddCookie(sessionCookie)
	sess, ok := auth.GetSession(authedReq)
	if !ok {
		t.Fatalf("expected valid session")
	}

	// 3. Create a project via POST /admin
	createForm := url.Values{}
	createForm.Set("csrf_token", sess.CSRFToken)
	createForm.Set("action", "create")
	createForm.Set("title", "Cloud Inventory API")
	createForm.Set("description", "RESTful microservice for inventory tracking in Go and PostgreSQL.")
	createForm.Set("technologies", "Go, PostgreSQL, Docker")
	createForm.Set("project_url", "https://my-go-app-1008298857757.asia-south1.run.app")
	createForm.Set("github_url", "https://github.com/Nikhil2006-stack/my-go-app")

	createReq := httptest.NewRequest(http.MethodPost, "/admin", strings.NewReader(createForm.Encode()))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(sessionCookie)
	createRec := httptest.NewRecorder()
	srv.AdminDashboardHandler(createRec, createReq)

	if createRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect after create, got %d", createRec.Code)
	}

	projects, err := store.ListProjects(context.Background())
	if err != nil || len(projects) != 3 {
		t.Fatalf("expected 3 projects after create, got %d (err=%v)", len(projects), err)
	}
	createdID := projects[len(projects)-1].ID

	// 4. Update the project via POST /admin
	updateForm := url.Values{}
	updateForm.Set("csrf_token", sess.CSRFToken)
	updateForm.Set("action", "update")
	updateForm.Set("id", strconv.Itoa(createdID))
	updateForm.Set("title", "Cloud Inventory API v2")
	updateForm.Set("description", "Updated description for the Cloud Inventory microservice.")
	updateForm.Set("technologies", "Go, PostgreSQL, Cloud Run")
	updateForm.Set("project_url", "https://my-go-app-1008298857757.asia-south1.run.app")
	updateForm.Set("github_url", "https://github.com/Nikhil2006-stack/my-go-app")

	updateReq := httptest.NewRequest(http.MethodPost, "/admin", strings.NewReader(updateForm.Encode()))
	updateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateReq.AddCookie(sessionCookie)
	updateRec := httptest.NewRecorder()
	srv.AdminDashboardHandler(updateRec, updateReq)

	updatedProj, err := store.GetProjectByID(context.Background(), createdID)
	if err != nil || updatedProj.Title != "Cloud Inventory API v2" {
		t.Fatalf("expected updated title 'Cloud Inventory API v2', got %q (err=%v)", updatedProj.Title, err)
	}

	// 5. Delete the project via POST /admin
	deleteForm := url.Values{}
	deleteForm.Set("csrf_token", sess.CSRFToken)
	deleteForm.Set("action", "delete")
	deleteForm.Set("id", strconv.Itoa(createdID))

	deleteReq := httptest.NewRequest(http.MethodPost, "/admin", strings.NewReader(deleteForm.Encode()))
	deleteReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteReq.AddCookie(sessionCookie)
	deleteRec := httptest.NewRecorder()
	srv.AdminDashboardHandler(deleteRec, deleteReq)

	afterDelete, _ := store.ListProjects(context.Background())
	if len(afterDelete) != 2 {
		t.Fatalf("expected 2 projects after delete, got %d", len(afterDelete))
	}
}

func TestHomeAndContactForm(t *testing.T) {
	store := database.NewFallbackStore()
	auth := middleware.NewAuthManager()
	srv := NewServer(store, auth, filepath.Join("..", "templates"))

	// 1. GET / renders public portfolio with projects, LinkedIn, GitHub, and resume links
	homeReq := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRec := httptest.NewRecorder()
	srv.HomeHandler(homeRec, homeReq)

	if homeRec.Code != http.StatusOK {
		t.Fatalf("GET / expected 200, got %d", homeRec.Code)
	}
	body := homeRec.Body.String()
	for _, mustContain := range []string{
		"Smart Task Management System",
		"Smart E-Commerce Platform",
		"https://www.linkedin.com/in/nikhil-k-925349338/",
		"https://github.com/Nikhil2006-stack/my-go-app",
		"/static/resume.pdf",
	} {
		if !strings.Contains(body, mustContain) {
			t.Fatalf("expected home page HTML to contain %q", mustContain)
		}
	}

	var csrfCookie *http.Cookie
	for _, c := range homeRec.Result().Cookies() {
		if c.Name == middleware.CSRFCookieName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatalf("expected public CSRF cookie on GET /")
	}

	// 2. POST /contact with valid input saves message
	form := url.Values{}
	form.Set("csrf_token", csrfCookie.Value)
	form.Set("name", "Priya Sharma")
	form.Set("email", "priya@example.com")
	form.Set("subject", "Full Stack Opportunity")
	form.Set("message", "Hi Nikhil, we loved your Go and PostgreSQL portfolio!")

	contactReq := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	contactReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	contactReq.Header.Set("Accept", "application/json")
	contactReq.AddCookie(csrfCookie)
	contactRec := httptest.NewRecorder()
	srv.ContactSubmitHandler(contactRec, contactReq)

	if contactRec.Code != http.StatusOK {
		t.Fatalf("POST /contact expected 200 OK, got %d (%s)", contactRec.Code, contactRec.Body.String())
	}

	msgs, err := store.ListContactMessages(context.Background())
	if err != nil || len(msgs) != 1 {
		t.Fatalf("expected 1 saved contact message, got %d (err=%v)", len(msgs), err)
	}
	if msgs[0].Email != "priya@example.com" {
		t.Fatalf("unexpected saved email: %q", msgs[0].Email)
	}

	// 3. Honeypot spam protection: if "website" field is filled, do not save spam
	spamForm := url.Values{}
	spamForm.Set("csrf_token", csrfCookie.Value)
	spamForm.Set("website", "http://spam-bot.example")
	spamForm.Set("name", "Spammer")
	spamForm.Set("email", "spam@example.com")
	spamForm.Set("subject", "Spam")
	spamForm.Set("message", "Buy cheap links right now!")

	spamReq := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(spamForm.Encode()))
	spamReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	spamReq.AddCookie(csrfCookie)
	spamRec := httptest.NewRecorder()
	srv.ContactSubmitHandler(spamRec, spamReq)

	msgsAfterSpam, _ := store.ListContactMessages(context.Background())
	if len(msgsAfterSpam) != 1 {
		t.Fatalf("expected honeypot spam message to be ignored, got %d messages", len(msgsAfterSpam))
	}
}
