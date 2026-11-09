
package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Project struct {
	ID           int
	Title        string
	Description  string
	Technologies string
	ProjectURL   string
	GithubURL    string
}

type PageData struct {
	Projects []Project
}

var db *pgxpool.Pool

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	db, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal("Database connection error:", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatal("Database ping failed:", err)
	}

	log.Println("Connected to PostgreSQL successfully!")

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	http.HandleFunc("/", homeHandler)
	http.Handle("/admin", adminAuth(http.HandlerFunc(adminHandler)))
	log.Println("Portfolio server running on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	rows, err := db.Query(r.Context(), `
		SELECT id, title, description, technologies, project_url, github_url
		FROM projects
		ORDER BY id DESC
	`)
	if err != nil {
		http.Error(w, "Could not load projects", http.StatusInternalServerError)
		log.Println(err)
		return
	}
	defer rows.Close()

	data := PageData{}

	for rows.Next() {
		var p Project
		err := rows.Scan(
			&p.ID,
			&p.Title,
			&p.Description,
			&p.Technologies,
			&p.ProjectURL,
			&p.GithubURL,
		)
		if err != nil {
			http.Error(w, "Could not read projects", http.StatusInternalServerError)
			log.Println(err)
			return
		}
		data.Projects = append(data.Projects, p)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Could not read projects", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, "Could not load website template", http.StatusInternalServerError)
		log.Println(err)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Println(err)
	}
}
