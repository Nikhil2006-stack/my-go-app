package database

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("record not found")

// Project represents a portfolio project stored in PostgreSQL.
type Project struct {
	ID           int
	Title        string
	Description  string
	Technologies string
	ProjectURL   string
	GithubURL    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TechList splits the comma-separated Technologies string into trimmed items for display.
func (p Project) TechList() []string {
	if strings.TrimSpace(p.Technologies) == "" {
		return nil
	}
	parts := strings.Split(p.Technologies, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ContactMessage represents a message submitted through the public contact form.
type ContactMessage struct {
	ID        int
	Name      string
	Email     string
	Subject   string
	Message   string
	IPAddress string
	IsRead    bool
	CreatedAt time.Time
}

// Store defines all database operations for projects and contact messages.
type Store interface {
	ListProjects(ctx context.Context) ([]Project, error)
	GetProjectByID(ctx context.Context, id int) (Project, error)
	CreateProject(ctx context.Context, p Project) (int, error)
	UpdateProject(ctx context.Context, p Project) error
	DeleteProject(ctx context.Context, id int) error

	CreateContactMessage(ctx context.Context, msg ContactMessage) (int, error)
	ListContactMessages(ctx context.Context) ([]ContactMessage, error)
	MarkContactMessageRead(ctx context.Context, id int, isRead bool) error
	DeleteContactMessage(ctx context.Context, id int) error

	IsPersistent() bool
	Close()
}

// DefaultProjects returns the user's initial portfolio projects used for seeding.
func DefaultProjects() []Project {
	return []Project{
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
			ProjectURL:   "",
			GithubURL:    "https://github.com/Nikhil2006-stack/my-go-app",
		},
	}
}

// BuildConnString constructs a PostgreSQL connection string from environment variables.
// It never assumes localhost in Cloud Run unless explicitly configured.
func BuildConnString() string {
	if dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); dbURL != "" {
		return dbURL
	}

	host := strings.TrimSpace(os.Getenv("DB_HOST"))
	user := strings.TrimSpace(os.Getenv("DB_USER"))
	password := os.Getenv("DB_PASSWORD")
	dbName := strings.TrimSpace(os.Getenv("DB_NAME"))
	port := strings.TrimSpace(os.Getenv("DB_PORT"))
	sslMode := strings.TrimSpace(os.Getenv("DB_SSLMODE"))

	if host == "" || user == "" || dbName == "" {
		return ""
	}
	if port == "" {
		port = "5432"
	}
	if sslMode == "" {
		if strings.HasPrefix(host, "/cloudsql/") {
			sslMode = "disable"
		} else if host == "localhost" || host == "127.0.0.1" {
			sslMode = "disable"
		} else {
			sslMode = "require"
		}
	}

	// Support Cloud SQL Unix socket paths (e.g., /cloudsql/project:region:instance)
	if strings.HasPrefix(host, "/cloudsql/") {
		q := url.Values{}
		q.Set("host", host)
		u := &url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(user, password),
			Path:     "/" + dbName,
			RawQuery: q.Encode(),
		}
		return u.String()
	}

	q := url.Values{}
	q.Set("sslmode", sslMode)
	u := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     fmt.Sprintf("%s:%s", host, port),
		Path:     "/" + dbName,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// InitStore connects to PostgreSQL when configured, runs SQL migrations and initial seeding.
// If no database environment variables are set (or if allowFallback is true and connection fails),
// it returns an in-memory fallback store so the application remains available.
func InitStore(ctx context.Context) (Store, *pgxpool.Pool, error) {
	connStr := BuildConnString()
	if connStr == "" {
		log.Println("WARNING: DATABASE_URL is not set. Using in-memory fallback store (changes will not persist across restarts).")
		return NewFallbackStore(), nil, nil
	}

	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid database configuration: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create PostgreSQL connection pool: %w", err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("unable to connect to PostgreSQL database: %w", err)
	}

	store := &PostgresStore{pool: pool}
	if err := store.MigrateAndSeed(connectCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("database migration failed: %w", err)
	}

	log.Println("Connected to PostgreSQL database and verified schema migrations.")
	return store, pool, nil
}

// PostgresStore implements Store backed by PostgreSQL using parameterized queries.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wraps an existing pgxpool.Pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) IsPersistent() bool {
	return true
}

func (s *PostgresStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// MigrateAndSeed executes schema.sql and seeds initial projects if the table is empty.
func (s *PostgresStore) MigrateAndSeed(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("executing schema.sql: %w", err)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM projects`).Scan(&count); err != nil {
		return fmt.Errorf("counting existing projects: %w", err)
	}

	if count == 0 {
		defaults := DefaultProjects()
		for _, p := range defaults {
			_, err := s.pool.Exec(ctx,
				`INSERT INTO projects (title, description, technologies, project_url, github_url)
				 VALUES ($1, $2, $3, $4, $5)`,
				p.Title, p.Description, p.Technologies, p.ProjectURL, p.GithubURL,
			)
			if err != nil {
				return fmt.Errorf("seeding initial project %q: %w", p.Title, err)
			}
		}
		log.Printf("Seeded %d default portfolio projects into PostgreSQL.", len(defaults))
	}

	return nil
}

func (s *PostgresStore) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, title, description, technologies, project_url, github_url, created_at, updated_at
		 FROM projects
		 ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID, &p.Title, &p.Description,
			&p.Technologies, &p.ProjectURL, &p.GithubURL,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *PostgresStore) GetProjectByID(ctx context.Context, id int) (Project, error) {
	var p Project
	err := s.pool.QueryRow(ctx,
		`SELECT id, title, description, technologies, project_url, github_url, created_at, updated_at
		 FROM projects
		 WHERE id = $1`, id,
	).Scan(
		&p.ID, &p.Title, &p.Description,
		&p.Technologies, &p.ProjectURL, &p.GithubURL,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return Project{}, ErrNotFound
	}
	return p, nil
}

func (s *PostgresStore) CreateProject(ctx context.Context, p Project) (int, error) {
	var id int
	err := s.pool.QueryRow(ctx,
		`INSERT INTO projects (title, description, technologies, project_url, github_url, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		 RETURNING id`,
		p.Title, p.Description, p.Technologies, p.ProjectURL, p.GithubURL,
	).Scan(&id)
	return id, err
}

func (s *PostgresStore) UpdateProject(ctx context.Context, p Project) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE projects
		 SET title = $1,
		     description = $2,
		     technologies = $3,
		     project_url = $4,
		     github_url = $5,
		     updated_at = NOW()
		 WHERE id = $6`,
		p.Title, p.Description, p.Technologies, p.ProjectURL, p.GithubURL, p.ID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteProject(ctx context.Context, id int) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateContactMessage(ctx context.Context, msg ContactMessage) (int, error) {
	var id int
	err := s.pool.QueryRow(ctx,
		`INSERT INTO contact_messages (name, email, subject, message, ip_address, is_read, created_at)
		 VALUES ($1, $2, $3, $4, $5, FALSE, NOW())
		 RETURNING id`,
		msg.Name, msg.Email, msg.Subject, msg.Message, msg.IPAddress,
	).Scan(&id)
	return id, err
}

func (s *PostgresStore) ListContactMessages(ctx context.Context) ([]ContactMessage, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, email, subject, message, ip_address, is_read, created_at
		 FROM contact_messages
		 ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ContactMessage
	for rows.Next() {
		var m ContactMessage
		if err := rows.Scan(
			&m.ID, &m.Name, &m.Email, &m.Subject,
			&m.Message, &m.IPAddress, &m.IsRead, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func (s *PostgresStore) MarkContactMessageRead(ctx context.Context, id int, isRead bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE contact_messages SET is_read = $1 WHERE id = $2`,
		isRead, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteContactMessage(ctx context.Context, id int) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM contact_messages WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// FallbackStore is a thread-safe in-memory implementation of Store used when DATABASE_URL is not set
// or during unit testing.
type FallbackStore struct {
	mu            sync.RWMutex
	projects      []Project
	nextProjectID int
	messages      []ContactMessage
	nextMessageID int
}

// NewFallbackStore creates a new FallbackStore seeded with DefaultProjects().
func NewFallbackStore() *FallbackStore {
	defaults := DefaultProjects()
	now := time.Now()
	for i := range defaults {
		defaults[i].CreatedAt = now
		defaults[i].UpdatedAt = now
	}
	return &FallbackStore{
		projects:      defaults,
		nextProjectID: len(defaults) + 1,
		nextMessageID: 1,
	}
}

func (f *FallbackStore) IsPersistent() bool {
	return false
}

func (f *FallbackStore) Close() {}

func (f *FallbackStore) ListProjects(_ context.Context) ([]Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]Project, len(f.projects))
	copy(out, f.projects)
	return out, nil
}

func (f *FallbackStore) GetProjectByID(_ context.Context, id int) (Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, p := range f.projects {
		if p.ID == id {
			return p, nil
		}
	}
	return Project{}, ErrNotFound
}

func (f *FallbackStore) CreateProject(_ context.Context, p Project) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	p.ID = f.nextProjectID
	f.nextProjectID++
	p.CreatedAt = now
	p.UpdatedAt = now
	f.projects = append(f.projects, p)
	return p.ID, nil
}

func (f *FallbackStore) UpdateProject(_ context.Context, updated Project) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.projects {
		if p.ID == updated.ID {
			updated.CreatedAt = p.CreatedAt
			updated.UpdatedAt = time.Now()
			f.projects[i] = updated
			return nil
		}
	}
	return ErrNotFound
}

func (f *FallbackStore) DeleteProject(_ context.Context, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, p := range f.projects {
		if p.ID == id {
			f.projects = append(f.projects[:i], f.projects[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (f *FallbackStore) CreateContactMessage(_ context.Context, msg ContactMessage) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	msg.ID = f.nextMessageID
	f.nextMessageID++
	msg.CreatedAt = time.Now()
	msg.IsRead = false
	f.messages = append([]ContactMessage{msg}, f.messages...)
	return msg.ID, nil
}

func (f *FallbackStore) ListContactMessages(_ context.Context) ([]ContactMessage, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]ContactMessage, len(f.messages))
	copy(out, f.messages)
	return out, nil
}

func (f *FallbackStore) MarkContactMessageRead(_ context.Context, id int, isRead bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.messages {
		if m.ID == id {
			f.messages[i].IsRead = isRead
			return nil
		}
	}
	return ErrNotFound
}

func (f *FallbackStore) DeleteContactMessage(_ context.Context, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.messages {
		if m.ID == id {
			f.messages = append(f.messages[:i], f.messages[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}
