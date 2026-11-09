
package main

import (
	"crypto/subtle"
	"html/template"
	"net/http"
	"os"
	"strconv"
)

func adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedUser := os.Getenv("ADMIN_USER")
		expectedPassword := os.Getenv("ADMIN_PASSWORD")

		if expectedUser == "" || expectedPassword == "" {
			http.Error(w, "Admin credentials are not configured.", http.StatusServiceUnavailable)
			return
		}

		user, password, ok := r.BasicAuth()

		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(expectedUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Portfolio Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func adminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}

		switch r.FormValue("action") {
		case "create":
			_, err := db.Exec(r.Context(),
				`INSERT INTO projects
				(title, description, technologies, project_url, github_url)
				VALUES ($1, $2, $3, $4, $5)`,
				r.FormValue("title"),
				r.FormValue("description"),
				r.FormValue("technologies"),
				r.FormValue("project_url"),
				r.FormValue("github_url"),
			)
			if err != nil {
				http.Error(w, "Could not create project", http.StatusInternalServerError)
				return
			}

		case "update":
			id, err := strconv.Atoi(r.FormValue("id"))
			if err != nil {
				http.Error(w, "Invalid project ID", http.StatusBadRequest)
				return
			}

			_, err = db.Exec(r.Context(),
				`UPDATE projects SET title=$1, description=$2,
				technologies=$3, project_url=$4, github_url=$5
				WHERE id=$6`,
				r.FormValue("title"),
				r.FormValue("description"),
				r.FormValue("technologies"),
				r.FormValue("project_url"),
				r.FormValue("github_url"),
				id,
			)
			if err != nil {
				http.Error(w, "Could not update project", http.StatusInternalServerError)
				return
			}

		case "delete":
			id, err := strconv.Atoi(r.FormValue("id"))
			if err != nil {
				http.Error(w, "Invalid project ID", http.StatusBadRequest)
				return
			}

			_, err = db.Exec(r.Context(),
				`DELETE FROM projects WHERE id=$1`, id)
			if err != nil {
				http.Error(w, "Could not delete project", http.StatusInternalServerError)
				return
			}

		default:
			http.Error(w, "Unknown action", http.StatusBadRequest)
			return
		}

		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := db.Query(r.Context(),
		`SELECT id, title, description, technologies, project_url, github_url
		FROM projects ORDER BY id DESC`)
	if err != nil {
		http.Error(w, "Could not load projects", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	data := PageData{}

	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.Title, &p.Description,
			&p.Technologies, &p.ProjectURL, &p.GithubURL,
		); err != nil {
			http.Error(w, "Could not read projects", http.StatusInternalServerError)
			return
		}
		data.Projects = append(data.Projects, p)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Could not read projects", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.ParseFiles("templates/admin.html")
	if err != nil {
		http.Error(w, "Could not load admin page", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		return
	}
}
