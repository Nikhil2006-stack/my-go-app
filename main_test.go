package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"my-go-app/database"
	"my-go-app/handlers"
	"my-go-app/middleware"
)

var csrfInputRe = regexp.MustCompile(`name="csrf_token"\s+value="([^"]+)"`)

func extractCSRFToken(t *testing.T, htmlBody string) string {
	t.Helper()
	m := csrfInputRe.FindStringSubmatch(htmlBody)
	if len(m) < 2 || m[1] == "" {
		t.Fatalf("could not find csrf_token in HTML body")
	}
	return m[1]
}

func TestEndToEndPortfolioAndAdminRoutes(t *testing.T) {
	t.Setenv("ADMIN_USER", "nikhil_admin")
	t.Setenv("ADMIN_PASSWORD", "PortfolioSecret#2026")
	t.Setenv("SESSION_SECRET", "e2e-test-session-secret-key-0123456789")

	store := database.NewFallbackStore()
	auth := middleware.NewAuthManager()
	srv := handlers.NewServer(store, auth, "templates")
	ts := httptest.NewServer(NewRouter(srv, "static"))
	defer ts.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Check GET / (public portfolio)
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	homeHTML := string(bodyBytes)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff header")
	}
	for _, link := range []string{
		"https://www.linkedin.com/in/nikhil-k-925349338/",
		"https://github.com/Nikhil2006-stack/my-go-app",
		"/static/resume.pdf",
		"Smart Task Management System",
	} {
		if !strings.Contains(homeHTML, link) {
			t.Fatalf("GET / missing expected content %q", link)
		}
	}

	publicCSRF := extractCSRFToken(t, homeHTML)

	// 2. Check static files (/static/resume.pdf, /static/style.css, /static/script.js)
	for _, staticPath := range []string{"/static/resume.pdf", "/static/style.css", "/static/script.js"} {
		sResp, err := client.Get(ts.URL + staticPath)
		if err != nil {
			t.Fatalf("GET %s: %v", staticPath, err)
		}
		b, _ := io.ReadAll(sResp.Body)
		sResp.Body.Close()
		if sResp.StatusCode != http.StatusOK || len(b) == 0 {
			t.Fatalf("GET %s expected 200 and non-empty body, got status=%d len=%d", staticPath, sResp.StatusCode, len(b))
		}
	}

	// 3. Check GET /healthz
	hResp, err := client.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	hResp.Body.Close()
	if hResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz expected 200, got %d", hResp.StatusCode)
	}

	// 4. Check 404 on unknown route
	notFoundResp, err := client.Get(ts.URL + "/non-existent-page")
	if err != nil {
		t.Fatalf("GET /non-existent-page: %v", err)
	}
	notFoundResp.Body.Close()
	if notFoundResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on unknown route, got %d", notFoundResp.StatusCode)
	}

	// 5. Submit public contact form (POST /contact)
	contactForm := url.Values{
		"csrf_token": {publicCSRF},
		"name":       {"Rahul Verma"},
		"email":      {"rahul@example.com"},
		"subject":    {"Backend Role Inquiry"},
		"message":    {"Hello Nikhil, I would love to discuss a Go backend role with you."},
	}
	cResp, err := client.PostForm(ts.URL+"/contact", contactForm)
	if err != nil {
		t.Fatalf("POST /contact: %v", err)
	}
	cResp.Body.Close()
	if cResp.StatusCode != http.StatusSeeOther || !strings.Contains(cResp.Header.Get("Location"), "contact=sent") {
		t.Fatalf("expected redirect to /?contact=sent#contact, got status=%d loc=%q", cResp.StatusCode, cResp.Header.Get("Location"))
	}

	// 6. Verify unauthenticated GET /admin redirects to /admin/login
	unauthResp, err := client.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin unauthenticated: %v", err)
	}
	unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusSeeOther || unauthResp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("expected redirect to /admin/login, got %d (%s)", unauthResp.StatusCode, unauthResp.Header.Get("Location"))
	}

	// 7. GET /admin/login and test invalid login first, then valid login
	loginPageResp, err := client.Get(ts.URL + "/admin/login")
	if err != nil {
		t.Fatalf("GET /admin/login: %v", err)
	}
	loginPageBytes, _ := io.ReadAll(loginPageResp.Body)
	loginPageResp.Body.Close()
	loginCSRF := extractCSRFToken(t, string(loginPageBytes))

	badLoginResp, err := client.PostForm(ts.URL+"/admin/login", url.Values{
		"csrf_token": {loginCSRF},
		"username":   {"nikhil_admin"},
		"password":   {"wrong-password"},
	})
	if err != nil {
		t.Fatalf("POST /admin/login (bad): %v", err)
	}
	badLoginResp.Body.Close()
	if badLoginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", badLoginResp.StatusCode)
	}

	goodLoginResp, err := client.PostForm(ts.URL+"/admin/login", url.Values{
		"csrf_token": {loginCSRF},
		"username":   {"nikhil_admin"},
		"password":   {"PortfolioSecret#2026"},
	})
	if err != nil {
		t.Fatalf("POST /admin/login (good): %v", err)
	}
	goodLoginResp.Body.Close()
	if goodLoginResp.StatusCode != http.StatusSeeOther || goodLoginResp.Header.Get("Location") != "/admin" {
		t.Fatalf("expected redirect to /admin after valid login, got %d (%s)", goodLoginResp.StatusCode, goodLoginResp.Header.Get("Location"))
	}

	// 8. Access GET /admin (authenticated) and verify contact message is visible
	adminResp, err := client.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin authenticated: %v", err)
	}
	adminBytes, _ := io.ReadAll(adminResp.Body)
	adminResp.Body.Close()
	adminHTML := string(adminBytes)
	if adminResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin expected 200, got %d", adminResp.StatusCode)
	}
	if !strings.Contains(adminHTML, "Backend Role Inquiry") || !strings.Contains(adminHTML, "rahul@example.com") {
		t.Fatalf("expected contact message to appear in admin dashboard")
	}
	adminCSRF := extractCSRFToken(t, adminHTML)

	// 9. Add a new project via POST /admin and verify it appears on public GET /
	addResp, err := client.PostForm(ts.URL+"/admin", url.Values{
		"csrf_token":   {adminCSRF},
		"action":       {"create"},
		"title":        {"Realtime Analytics Dashboard"},
		"description":  {"A real-time metrics dashboard powered by Go concurrency and PostgreSQL."},
		"technologies": {"Go, PostgreSQL, WebSockets"},
		"project_url":  {"https://my-go-app-1008298857757.asia-south1.run.app"},
		"github_url":   {"https://github.com/Nikhil2006-stack/my-go-app"},
	})
	if err != nil {
		t.Fatalf("POST /admin create: %v", err)
	}
	addResp.Body.Close()

	pubRespAfterAdd, _ := client.Get(ts.URL + "/")
	pubBytesAfterAdd, _ := io.ReadAll(pubRespAfterAdd.Body)
	pubRespAfterAdd.Body.Close()
	if !strings.Contains(string(pubBytesAfterAdd), "Realtime Analytics Dashboard") {
		t.Fatalf("expected newly created project to appear on public portfolio /")
	}

	// 10. Edit the project via POST /admin and verify update on public GET /
	projects, _ := store.ListProjects(context.Background())
	newProjID := projects[len(projects)-1].ID

	editResp, err := client.PostForm(ts.URL+"/admin", url.Values{
		"csrf_token":   {adminCSRF},
		"action":       {"update"},
		"id":           {strconv.Itoa(newProjID)},
		"title":        {"Realtime Cloud Analytics v2"},
		"description":  {"Updated real-time metrics dashboard powered by Go and PostgreSQL."},
		"technologies": {"Go, PostgreSQL, Cloud Run"},
		"project_url":  {"https://my-go-app-1008298857757.asia-south1.run.app"},
		"github_url":   {"https://github.com/Nikhil2006-stack/my-go-app"},
	})
	if err != nil {
		t.Fatalf("POST /admin update: %v", err)
	}
	editResp.Body.Close()

	pubRespAfterEdit, _ := client.Get(ts.URL + "/")
	pubBytesAfterEdit, _ := io.ReadAll(pubRespAfterEdit.Body)
	pubRespAfterEdit.Body.Close()
	if !strings.Contains(string(pubBytesAfterEdit), "Realtime Cloud Analytics v2") {
		t.Fatalf("expected updated project title on public portfolio /")
	}

	// 11. Delete the project via POST /admin and verify removal on public GET /
	delResp, err := client.PostForm(ts.URL+"/admin", url.Values{
		"csrf_token": {adminCSRF},
		"action":     {"delete"},
		"id":         {strconv.Itoa(newProjID)},
	})
	if err != nil {
		t.Fatalf("POST /admin delete: %v", err)
	}
	delResp.Body.Close()

	pubRespAfterDel, _ := client.Get(ts.URL + "/")
	pubBytesAfterDel, _ := io.ReadAll(pubRespAfterDel.Body)
	pubRespAfterDel.Body.Close()
	if strings.Contains(string(pubBytesAfterDel), "Realtime Cloud Analytics v2") {
		t.Fatalf("expected deleted project to be removed from public portfolio /")
	}

	// 12. Logout via POST /admin/logout and verify /admin is protected again
	logoutResp, err := client.PostForm(ts.URL+"/admin/logout", url.Values{
		"csrf_token": {adminCSRF},
	})
	if err != nil {
		t.Fatalf("POST /admin/logout: %v", err)
	}
	logoutResp.Body.Close()
	if logoutResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on logout, got %d", logoutResp.StatusCode)
	}

	afterLogoutResp, err := client.Get(ts.URL + "/admin")
	if err != nil {
		t.Fatalf("GET /admin after logout: %v", err)
	}
	afterLogoutResp.Body.Close()
	if afterLogoutResp.StatusCode != http.StatusSeeOther || afterLogoutResp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("expected redirect to /admin/login after logout, got %d", afterLogoutResp.StatusCode)
	}
}
