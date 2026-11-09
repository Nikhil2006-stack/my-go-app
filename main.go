package main

import (
	"html/template"
	"log"
	"net/http"
	"os"

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

// Kept so admin.go still compiles.
// The online version does not connect to a database.
var db *pgxpool.Pool

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", homeHandler)

	log.Println("Portfolio server running on port", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := PageData{
		Projects: []Project{
			{
				ID:           1,
				Title:        "Smart Task Management System",
				Description:  "A full-stack task management platform featuring secure user authentication, personalized dashboards, task creation and editing, priority-based organization, deadline tracking, task status management, and responsive design. Built with a scalable Go backend and PostgreSQL database.",
				Technologies: "Go, PostgreSQL, HTML5, CSS3, JavaScript, REST API, SQL, Authentication",
				ProjectURL:   "https://my-go-app-1008298857757.asia-south1.run.app",
				GithubURL:    "https://github.com/Nikhil2006-stack/my-go-app",
			},
			{
				ID:           2,
				Title:        "Smart E-Commerce Platform",
				Description:  "A full-stack e-commerce platform where users can browse products, add items to cart, manage orders, and securely register and log in. Includes a responsive user interface and database integration.",
				Technologies: "Go, PostgreSQL, HTML, CSS, JavaScript, REST API",
				GithubURL:    "https://github.com/Nikhil2006-stack/my-go-app",
			},
		},
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
