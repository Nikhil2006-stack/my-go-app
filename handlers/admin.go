package handlers

import (
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"my-go-app/database"
	"my-go-app/middleware"
)

// Server bundles dependencies for all HTTP route handlers.
type Server struct {
	Store          database.Store
	Auth           *middleware.AuthManager
	LoginLimiter   *middleware.RateLimiter
	ContactLimiter *middleware.RateLimiter
	TemplateDir    string
}

// NewServer constructs a Server with sensible defaults for rate limiting and templates.
func NewServer(store database.Store, auth *middleware.AuthManager, templateDir string) *Server {
	if templateDir == "" {
		templateDir = "templates"
	}
	return &Server{
		Store:          store,
		Auth:           auth,
		LoginLimiter:   middleware.NewRateLimiter(8, 10*time.Minute),
		ContactLimiter: middleware.NewRateLimiter(5, 10*time.Minute),
		TemplateDir:    templateDir,
	}
}

// AdminLoginData holds template data for templates/admin_login.html.
type AdminLoginData struct {
	CSRFToken    string
	Error        string
	Notice       string
	IsConfigured bool
	Username     string
}

// AdminPageData holds template data for templates/admin.html.
type AdminPageData struct {
	AdminUser     string
	CSRFToken     string
	IsPersistent  bool
	Projects      []database.Project
	Messages      []database.ContactMessage
	TotalProjects int
	TotalMessages int
	UnreadCount   int
	SuccessMsg    string
	ErrorMsg      string
}

// AdminLoginHandler serves GET /admin/login and processes POST /admin/login.
func (s *Server) AdminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.Auth.GetSession(r); ok && r.Method == http.MethodGet {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")

	switch r.Method {
	case http.MethodGet:
		csrfToken := s.Auth.EnsurePublicCSRFToken(w, r)
		data := AdminLoginData{
			CSRFToken:    csrfToken,
			IsConfigured: s.Auth.IsConfigured(),
		}
		if r.URL.Query().Get("logged_out") == "1" {
			data.Notice = "You have been securely logged out."
		}
		s.renderLoginTemplate(w, http.StatusOK, data)

	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form submission", http.StatusBadRequest)
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")

		if !s.Auth.IsConfigured() {
			csrfToken := s.Auth.EnsurePublicCSRFToken(w, r)
			s.renderLoginTemplate(w, http.StatusServiceUnavailable, AdminLoginData{
				CSRFToken:    csrfToken,
				IsConfigured: false,
				Error:        "Administrator credentials are not configured. Set ADMIN_USER and ADMIN_PASSWORD (or ADMIN_PASSWORD_HASH) in environment variables.",
			})
			return
		}

		if !s.Auth.VerifyPublicCSRF(r) {
			csrfToken := s.Auth.EnsurePublicCSRFToken(w, r)
			s.renderLoginTemplate(w, http.StatusForbidden, AdminLoginData{
				CSRFToken:    csrfToken,
				IsConfigured: true,
				Username:     username,
				Error:        "Your login form session expired. Please try again.",
			})
			return
		}

		ip := middleware.ClientIP(r)
		if !s.LoginLimiter.Allow(ip) {
			csrfToken := s.Auth.EnsurePublicCSRFToken(w, r)
			s.renderLoginTemplate(w, http.StatusTooManyRequests, AdminLoginData{
				CSRFToken:    csrfToken,
				IsConfigured: true,
				Username:     username,
				Error:        "Too many login attempts from your IP address. Please wait a few minutes and try again.",
			})
			return
		}

		if !s.Auth.Authenticate(username, password) {
			csrfToken := s.Auth.EnsurePublicCSRFToken(w, r)
			s.renderLoginTemplate(w, http.StatusUnauthorized, AdminLoginData{
				CSRFToken:    csrfToken,
				IsConfigured: true,
				Username:     username,
				Error:        "Invalid administrator username or password.",
			})
			return
		}

		if _, err := s.Auth.CreateSession(w, r, username); err != nil {
			log.Println("ERROR creating admin session:", err)
			http.Error(w, "Could not start session", http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/admin", http.StatusSeeOther)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// AdminLogoutHandler logs out the administrator and redirects to /admin/login.
func (s *Server) AdminLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		if !s.Auth.VerifySessionCSRF(r) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}
	}
	s.Auth.DestroySession(w, r)
	http.Redirect(w, r, "/admin/login?logged_out=1", http.StatusSeeOther)
}

// AdminDashboardHandler serves GET /admin and processes POST /admin actions.
// Must be wrapped with s.Auth.RequireAuth.
func (s *Server) AdminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.Auth.GetSession(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodPost {
		s.handleAdminPost(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projects, err := s.Store.ListProjects(r.Context())
	if err != nil {
		log.Println("ERROR loading projects for admin:", err)
		http.Error(w, "Could not load projects", http.StatusInternalServerError)
		return
	}

	messages, err := s.Store.ListContactMessages(r.Context())
	if err != nil {
		log.Println("ERROR loading contact messages for admin:", err)
		http.Error(w, "Could not load contact messages", http.StatusInternalServerError)
		return
	}

	unread := 0
	for _, m := range messages {
		if !m.IsRead {
			unread++
		}
	}

	data := AdminPageData{
		AdminUser:     sess.Username,
		CSRFToken:     sess.CSRFToken,
		IsPersistent:  s.Store.IsPersistent(),
		Projects:      projects,
		Messages:      messages,
		TotalProjects: len(projects),
		TotalMessages: len(messages),
		UnreadCount:   unread,
		SuccessMsg:    r.URL.Query().Get("msg"),
		ErrorMsg:      r.URL.Query().Get("err"),
	}

	tmplPath := filepath.Join(s.TemplateDir, "admin.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Println("ERROR parsing admin template:", err)
		http.Error(w, "Could not load admin dashboard template", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Println("ERROR rendering admin template:", err)
	}
}

func (s *Server) handleAdminPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		s.redirectAdminWithError(w, r, "Invalid form submission.")
		return
	}

	if !s.Auth.VerifySessionCSRF(r) {
		http.Error(w, "Invalid or missing CSRF token", http.StatusForbidden)
		return
	}

	action := strings.TrimSpace(r.FormValue("action"))

	switch action {
	case "create":
		proj, err := ValidateProjectInput(
			r.FormValue("title"),
			r.FormValue("description"),
			r.FormValue("technologies"),
			r.FormValue("project_url"),
			r.FormValue("github_url"),
		)
		if err != nil {
			s.redirectAdminWithError(w, r, err.Error())
			return
		}
		if _, err := s.Store.CreateProject(r.Context(), proj); err != nil {
			log.Println("ERROR creating project:", err)
			s.redirectAdminWithError(w, r, "Database error while creating project.")
			return
		}
		s.redirectAdminWithSuccess(w, r, "Project added successfully.")

	case "update":
		id, err := strconv.Atoi(strings.TrimSpace(r.FormValue("id")))
		if err != nil || id <= 0 {
			s.redirectAdminWithError(w, r, "Invalid project ID.")
			return
		}
		proj, err := ValidateProjectInput(
			r.FormValue("title"),
			r.FormValue("description"),
			r.FormValue("technologies"),
			r.FormValue("project_url"),
			r.FormValue("github_url"),
		)
		if err != nil {
			s.redirectAdminWithError(w, r, err.Error())
			return
		}
		proj.ID = id
		if err := s.Store.UpdateProject(r.Context(), proj); err != nil {
			log.Println("ERROR updating project:", err)
			s.redirectAdminWithError(w, r, "Could not update project.")
			return
		}
		s.redirectAdminWithSuccess(w, r, "Project updated successfully.")

	case "delete":
		id, err := strconv.Atoi(strings.TrimSpace(r.FormValue("id")))
		if err != nil || id <= 0 {
			s.redirectAdminWithError(w, r, "Invalid project ID.")
			return
		}
		if err := s.Store.DeleteProject(r.Context(), id); err != nil {
			log.Println("ERROR deleting project:", err)
			s.redirectAdminWithError(w, r, "Could not delete project.")
			return
		}
		s.redirectAdminWithSuccess(w, r, "Project deleted.")

	case "mark_read", "mark_unread":
		id, err := strconv.Atoi(strings.TrimSpace(r.FormValue("id")))
		if err != nil || id <= 0 {
			s.redirectAdminWithError(w, r, "Invalid message ID.")
			return
		}
		isRead := action == "mark_read"
		if err := s.Store.MarkContactMessageRead(r.Context(), id, isRead); err != nil {
			log.Println("ERROR updating message status:", err)
			s.redirectAdminWithError(w, r, "Could not update message status.")
			return
		}
		if isRead {
			s.redirectAdminWithSuccess(w, r, "Message marked as read.")
		} else {
			s.redirectAdminWithSuccess(w, r, "Message marked as unread.")
		}

	case "delete_message":
		id, err := strconv.Atoi(strings.TrimSpace(r.FormValue("id")))
		if err != nil || id <= 0 {
			s.redirectAdminWithError(w, r, "Invalid message ID.")
			return
		}
		if err := s.Store.DeleteContactMessage(r.Context(), id); err != nil {
			log.Println("ERROR deleting contact message:", err)
			s.redirectAdminWithError(w, r, "Could not delete contact message.")
			return
		}
		s.redirectAdminWithSuccess(w, r, "Contact message deleted.")

	default:
		s.redirectAdminWithError(w, r, "Unknown action requested.")
	}
}

func (s *Server) redirectAdminWithSuccess(w http.ResponseWriter, r *http.Request, msg string) {
	q := url.Values{}
	q.Set("msg", msg)
	http.Redirect(w, r, "/admin?"+q.Encode(), http.StatusSeeOther)
}

func (s *Server) redirectAdminWithError(w http.ResponseWriter, r *http.Request, errMsg string) {
	q := url.Values{}
	q.Set("err", errMsg)
	http.Redirect(w, r, "/admin?"+q.Encode(), http.StatusSeeOther)
}

func (s *Server) renderLoginTemplate(w http.ResponseWriter, status int, data AdminLoginData) {
	tmplPath := filepath.Join(s.TemplateDir, "admin_login.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Println("ERROR parsing admin_login template:", err)
		http.Error(w, "Could not load login page", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	if err := tmpl.Execute(w, data); err != nil {
		log.Println("ERROR executing admin_login template:", err)
	}
}
