package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"my-go-app/database"
	"my-go-app/middleware"
)

// PublicPageData holds template data for the public portfolio page (templates/index.html).
type PublicPageData struct {
	Projects       []database.Project
	CSRFToken      string
	ContactSuccess string
	ContactError   string
}

// HomeHandler serves GET / with dynamic projects from PostgreSQL.
func (s *Server) HomeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	projects, err := s.Store.ListProjects(r.Context())
	if err != nil {
		log.Println("ERROR loading projects for home page:", err)
		projects = database.DefaultProjects()
	}

	csrfToken := s.Auth.EnsurePublicCSRFToken(w, r)

	data := PublicPageData{
		Projects:  projects,
		CSRFToken: csrfToken,
	}
	if r.URL.Query().Get("contact") == "sent" {
		data.ContactSuccess = "Thank you! Your message has been sent successfully. I will get back to you soon."
	}
	if errMsg := strings.TrimSpace(r.URL.Query().Get("contact_error")); errMsg != "" {
		data.ContactError = errMsg
	}

	funcMap := template.FuncMap{
		"isOdd": func(i int) bool { return i%2 == 1 },
	}

	tmplPath := filepath.Join(s.TemplateDir, "index.html")
	tmpl, err := template.New("index.html").Funcs(funcMap).ParseFiles(tmplPath)
	if err != nil {
		log.Println("ERROR parsing index.html template:", err)
		http.Error(w, "Could not load website template", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Println("ERROR rendering index.html:", err)
	}
}

// ContactSubmitHandler handles POST /contact from the public portfolio contact form.
func (s *Server) ContactSubmitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		s.respondContactResult(w, r, http.StatusBadRequest, false, "Invalid form submission.")
		return
	}

	// Honeypot spam check: if the hidden "website" field is filled in by a bot, silently succeed.
	if strings.TrimSpace(r.FormValue("website")) != "" {
		s.respondContactResult(w, r, http.StatusOK, true, "Thank you! Your message has been sent successfully.")
		return
	}

	if !s.Auth.VerifyPublicCSRF(r) {
		s.respondContactResult(w, r, http.StatusForbidden, false, "Form session expired. Please refresh the page and try again.")
		return
	}

	ip := middleware.ClientIP(r)
	if !s.ContactLimiter.Allow(ip) {
		s.respondContactResult(w, r, http.StatusTooManyRequests, false, "Too many messages sent recently. Please wait a few minutes before trying again.")
		return
	}

	msg, err := ValidateContactInput(
		r.FormValue("name"),
		r.FormValue("email"),
		r.FormValue("subject"),
		r.FormValue("message"),
	)
	if err != nil {
		s.respondContactResult(w, r, http.StatusBadRequest, false, err.Error())
		return
	}
	msg.IPAddress = ip

	if _, err := s.Store.CreateContactMessage(r.Context(), msg); err != nil {
		log.Println("ERROR saving contact message:", err)
		s.respondContactResult(w, r, http.StatusInternalServerError, false, "Could not save your message right now. Please try again later.")
		return
	}

	s.respondContactResult(w, r, http.StatusOK, true, "Thank you! Your message has been sent successfully. I will get back to you soon.")
}

// HealthHandler serves GET /healthz for Cloud Run health checks.
func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"persistent": s.Store.IsPersistent(),
	})
}

func (s *Server) respondContactResult(w http.ResponseWriter, r *http.Request, status int, ok bool, message string) {
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      ok,
			"message": message,
		})
		return
	}

	if ok {
		http.Redirect(w, r, "/?contact=sent#contact", http.StatusSeeOther)
		return
	}
	q := url.Values{}
	q.Set("contact_error", message)
	http.Redirect(w, r, "/?"+q.Encode()+"#contact", http.StatusSeeOther)
}
