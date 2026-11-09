package handlers

import (
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"my-go-app/database"
)

// ValidateProjectInput trims and validates a Project before saving to the database.
func ValidateProjectInput(title, description, technologies, projectURL, githubURL string) (database.Project, error) {
	p := database.Project{
		Title:        strings.TrimSpace(title),
		Description:  strings.TrimSpace(description),
		Technologies: strings.TrimSpace(technologies),
		ProjectURL:   strings.TrimSpace(projectURL),
		GithubURL:    strings.TrimSpace(githubURL),
	}

	titleLen := utf8.RuneCountInString(p.Title)
	if titleLen < 2 || titleLen > 150 {
		return database.Project{}, errors.New("project title must be between 2 and 150 characters")
	}

	descLen := utf8.RuneCountInString(p.Description)
	if descLen < 10 || descLen > 2000 {
		return database.Project{}, errors.New("project description must be between 10 and 2000 characters")
	}

	techLen := utf8.RuneCountInString(p.Technologies)
	if techLen > 300 {
		return database.Project{}, errors.New("technologies must be 300 characters or fewer")
	}

	if p.ProjectURL != "" {
		cleanURL, err := validateSafeHTTPURL(p.ProjectURL)
		if err != nil {
			return database.Project{}, errors.New("live demo URL must be a valid http:// or https:// URL")
		}
		p.ProjectURL = cleanURL
	}

	if p.GithubURL != "" {
		cleanURL, err := validateSafeHTTPURL(p.GithubURL)
		if err != nil {
			return database.Project{}, errors.New("GitHub URL must be a valid http:// or https:// URL")
		}
		p.GithubURL = cleanURL
	}

	return p, nil
}

// ValidateContactInput trims and validates a public contact form submission.
func ValidateContactInput(name, email, subject, message string) (database.ContactMessage, error) {
	msg := database.ContactMessage{
		Name:    strings.TrimSpace(name),
		Email:   strings.TrimSpace(email),
		Subject: strings.TrimSpace(subject),
		Message: strings.TrimSpace(message),
	}

	nameLen := utf8.RuneCountInString(msg.Name)
	if nameLen < 2 || nameLen > 120 {
		return database.ContactMessage{}, errors.New("name must be between 2 and 120 characters")
	}

	emailLen := utf8.RuneCountInString(msg.Email)
	if emailLen < 5 || emailLen > 254 {
		return database.ContactMessage{}, errors.New("please enter a valid email address")
	}
	parsedEmail, err := mail.ParseAddress(msg.Email)
	if err != nil || parsedEmail.Address == "" || !strings.Contains(parsedEmail.Address, ".") {
		return database.ContactMessage{}, errors.New("please enter a valid email address")
	}
	msg.Email = parsedEmail.Address

	subjLen := utf8.RuneCountInString(msg.Subject)
	if subjLen < 2 || subjLen > 200 {
		return database.ContactMessage{}, errors.New("subject must be between 2 and 200 characters")
	}

	bodyLen := utf8.RuneCountInString(msg.Message)
	if bodyLen < 10 || bodyLen > 4000 {
		return database.ContactMessage{}, errors.New("message must be between 10 and 4000 characters")
	}

	return msg, nil
}

func validateSafeHTTPURL(raw string) (string, error) {
	if utf8.RuneCountInString(raw) > 500 {
		return "", errors.New("URL is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("invalid URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("unsupported URL scheme")
	}
	return u.String(), nil
}
