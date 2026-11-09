package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildConnString_EmptyDoesNotAssumeLocalhost(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("DB_NAME", "")

	got := BuildConnString()
	if got != "" {
		t.Fatalf("expected empty connection string when env vars are unset, got %q", got)
	}
}

func TestBuildConnString_DatabaseURL(t *testing.T) {
	want := "postgres://user:pass@ep-demo.aws.neon.tech/portfolio?sslmode=require"
	t.Setenv("DATABASE_URL", want)

	if got := BuildConnString(); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildConnString_IndividualVars(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "db.example.com")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "nikhil")
	t.Setenv("DB_PASSWORD", "s3cr3t")
	t.Setenv("DB_NAME", "portfolio")
	t.Setenv("DB_SSLMODE", "")

	got := BuildConnString()
	if !strings.Contains(got, "db.example.com:5432/portfolio") || !strings.Contains(got, "sslmode=require") {
		t.Fatalf("unexpected connection string: %q", got)
	}
}

func TestTechList(t *testing.T) {
	p := Project{Technologies: "Go, PostgreSQL, HTML5, CSS3, "}
	list := p.TechList()
	if len(list) != 4 {
		t.Fatalf("expected 4 tech items, got %d (%v)", len(list), list)
	}
	if list[0] != "Go" || list[1] != "PostgreSQL" {
		t.Fatalf("unexpected tech items: %v", list)
	}
}

func TestFallbackStoreCRUD(t *testing.T) {
	ctx := context.Background()
	store := NewFallbackStore()
	defer store.Close()

	initial, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(initial) != 2 {
		t.Fatalf("expected 2 seeded default projects, got %d", len(initial))
	}

	newID, err := store.CreateProject(ctx, Project{
		Title:        "Test Project",
		Description:  "Test Description",
		Technologies: "Go, Docker",
		ProjectURL:   "https://example.com",
		GithubURL:    "https://github.com/example/repo",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	got, err := store.GetProjectByID(ctx, newID)
	if err != nil {
		t.Fatalf("GetProjectByID failed: %v", err)
	}
	if got.Title != "Test Project" {
		t.Fatalf("expected title 'Test Project', got %q", got.Title)
	}

	got.Title = "Updated Project"
	if err := store.UpdateProject(ctx, got); err != nil {
		t.Fatalf("UpdateProject failed: %v", err)
	}

	updated, err := store.GetProjectByID(ctx, newID)
	if err != nil || updated.Title != "Updated Project" {
		t.Fatalf("expected updated title, got %q (err=%v)", updated.Title, err)
	}

	if err := store.DeleteProject(ctx, newID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
	if _, err := store.GetProjectByID(ctx, newID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	msgID, err := store.CreateContactMessage(ctx, ContactMessage{
		Name:      "Alice",
		Email:     "alice@example.com",
		Subject:   "Hello",
		Message:   "Great portfolio!",
		IPAddress: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("CreateContactMessage failed: %v", err)
	}

	if err := store.MarkContactMessageRead(ctx, msgID, true); err != nil {
		t.Fatalf("MarkContactMessageRead failed: %v", err)
	}

	msgs, err := store.ListContactMessages(ctx)
	if err != nil || len(msgs) != 1 || !msgs[0].IsRead {
		t.Fatalf("unexpected messages state: %+v (err=%v)", msgs, err)
	}

	if err := store.DeleteContactMessage(ctx, msgID); err != nil {
		t.Fatalf("DeleteContactMessage failed: %v", err)
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := "# comment\nTEST_KEY_STAGE1=\"hello_world\"\nEXISTING_KEY=should_not_override\n"
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		t.Fatalf("write temp .env: %v", err)
	}

	t.Setenv("EXISTING_KEY", "original_value")
	_ = os.Unsetenv("TEST_KEY_STAGE1")
	defer os.Unsetenv("TEST_KEY_STAGE1")

	if err := LoadEnvFile(envPath); err != nil {
		t.Fatalf("LoadEnvFile failed: %v", err)
	}
	if got := os.Getenv("TEST_KEY_STAGE1"); got != "hello_world" {
		t.Fatalf("expected 'hello_world', got %q", got)
	}
	if got := os.Getenv("EXISTING_KEY"); got != "original_value" {
		t.Fatalf("expected existing env var to be preserved, got %q", got)
	}
}
