package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"my-go-app/database"
	"my-go-app/handlers"
	"my-go-app/middleware"
)

func main() {
	// Load local .env file if present (never overrides Cloud Run environment variables).
	if err := database.LoadEnvFile(".env"); err != nil {
		log.Println("Notice: could not read .env file:", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ctx := context.Background()
	store, _, err := database.InitStore(ctx)
	if err != nil {
		log.Printf("WARNING: PostgreSQL initialization failed (%v). Falling back to in-memory store.", err)
		store = database.NewFallbackStore()
	}
	defer store.Close()

	auth := middleware.NewAuthManager()
	if !auth.IsConfigured() {
		log.Println("NOTICE: ADMIN_USER / ADMIN_PASSWORD not set yet. Set them in .env or Cloud Run env vars to enable admin login.")
	}

	srv := handlers.NewServer(store, auth, "templates")
	handler := NewRouter(srv, "static")

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Println("Portfolio server running on port", port)
	log.Fatal(httpServer.ListenAndServe())
}

// NewRouter registers all public, static, health, and protected admin routes.
func NewRouter(srv *handlers.Server, staticDir string) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.HandleFunc("/", srv.HomeHandler)
	mux.HandleFunc("/contact", srv.ContactSubmitHandler)
	mux.HandleFunc("/healthz", srv.HealthHandler)

	// Admin authentication & protected dashboard routes
	mux.HandleFunc("/admin/login", srv.AdminLoginHandler)
	mux.HandleFunc("/admin/logout", srv.AdminLogoutHandler)
	mux.HandleFunc("/admin", srv.Auth.RequireAuth(srv.AdminDashboardHandler))

	return middleware.SecurityHeaders(mux)
}
