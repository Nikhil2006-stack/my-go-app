# Personal Portfolio Website — Nikhil K.

A full-stack personal portfolio website built with **Go**, **PostgreSQL**, **HTML5**, **CSS3**, and **JavaScript**, designed for **Google Cloud Run**.

- **Live Website:** https://my-go-app-1008298857757.asia-south1.run.app
- **GitHub Repository:** https://github.com/Nikhil2006-stack/my-go-app

---

## Features

1. **Public Portfolio (`/`)**
   - Responsive dark/light theme UI with Hero, Stats, About, Skills, Featured Projects, and Contact sections.
   - Dynamic project cards loaded from PostgreSQL (`projects` table).
   - Functional Contact Form (`POST /contact`) with frontend & backend validation, CSRF protection, honeypot spam protection, and IP rate limiting.
   - Working links to LinkedIn, GitHub, email, and `/static/resume.pdf`.

2. **Admin Authentication & Dashboard (`/admin/login`, `/admin`)**
   - Protected `/admin` route using signed `HttpOnly` session cookies (12-hour expiration) and PBKDF2-HMAC-SHA256 password hashing.
   - Add, edit, and delete portfolio projects dynamically.
   - View, mark read/unread, reply to, and delete visitor contact messages.

3. **PostgreSQL Integration & Cloud Run Safety**
   - Embedded SQL schema migrations (`database/schema.sql`) and automatic initial project seeding.
   - Safe in-memory fallback if `DATABASE_URL` is not configured yet, ensuring the site never crashes on Cloud Run.

---

## Quick Start (Windows CMD)

1. Copy `.env.example` to `.env`:
   ```cmd
   copy .env.example .env
   ```
2. Edit `.env` and set your `ADMIN_USER`, `ADMIN_PASSWORD`, `SESSION_SECRET`, and (optionally) `DATABASE_URL`.
3. Run tests:
   ```cmd
   go test -v ./...
   ```
4. Start the server:
   ```cmd
   go run .
   ```
5. Open in your browser:
   - Public Portfolio: `http://localhost:8080`
   - Admin Dashboard: `http://localhost:8080/admin`
