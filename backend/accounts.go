package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User is the public shape of an account. PasswordHash is never serialized.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Bio          string    `json:"bio"`
	CreatedAt    time.Time `json:"createdAt"`
}

const (
	sessionDuration = 30 * 24 * time.Hour
	usernameMinLen  = 3
	usernameMaxLen  = 32
	passwordMinLen  = 8
	passwordMaxLen  = 72 // bcrypt only hashes the first 72 bytes
	bioMaxLen       = 500
)

var (
	ErrUserExists   = errors.New("username already taken")
	ErrInvalidLogin = errors.New("invalid username or password")
	ErrUnauthorized = errors.New("authentication required")
)

func validUsername(s string) bool {
	if len(s) < usernameMinLen || len(s) > usernameMaxLen {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validateCredentials(username, password string) error {
	username = strings.TrimSpace(username)
	if !validUsername(username) {
		return fmt.Errorf("username must be %d-%d characters (letters, digits, _ or -)", usernameMinLen, usernameMaxLen)
	}
	if len(password) < passwordMinLen || len(password) > passwordMaxLen {
		return fmt.Errorf("password must be %d-%d characters", passwordMinLen, passwordMaxLen)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateUser validates credentials, hashes the password with bcrypt and
// inserts the account. The username is stored case-insensitively.
func (s *Store) CreateUser(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if err := validateCredentials(username, password); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	u := User{ID: newID(), Username: username, CreatedAt: time.Now().UTC()}
	_, err = s.db.Exec(
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Username, string(hash), formatTime(u.CreatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrUserExists
		}
		return User{}, err
	}
	return u, nil
}

// LoginOrRegister authenticates username/password, creating the account when
// the username is not taken. created reports whether a new account was made.
// An existing username with the wrong password yields ErrInvalidLogin.
func (s *Store) LoginOrRegister(username, password string) (u User, created bool, err error) {
	username = strings.TrimSpace(username)
	if err := validateCredentials(username, password); err != nil {
		return User{}, false, err
	}
	u, err = s.Authenticate(username, password)
	if err == nil {
		return u, false, nil
	}
	if !errors.Is(err, ErrInvalidLogin) {
		return User{}, false, err
	}
	u, err = s.CreateUser(username, password)
	if err != nil {
		if errors.Is(err, ErrUserExists) {
			// The username exists but the password did not match.
			return User{}, false, ErrInvalidLogin
		}
		return User{}, false, err
	}
	return u, true, nil
}

// GetUser returns the account with the given ID.
func (s *Store) GetUser(id string) (User, error) {
	if !validID(id) {
		return User{}, ErrUnauthorized
	}
	var u User
	var created string
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, bio, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Bio, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUnauthorized
		}
		return User{}, err
	}
	if u.CreatedAt, err = parseTime(created); err != nil {
		return User{}, err
	}
	return u, nil
}

// GetUserByUsername looks up an account by its case-insensitive username.
// Returns sql.ErrNoRows when no such account exists.
func (s *Store) GetUserByUsername(username string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, sql.ErrNoRows
	}
	var u User
	var created string
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, bio, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Bio, &created)
	if err != nil {
		return User{}, err
	}
	if u.CreatedAt, err = parseTime(created); err != nil {
		return User{}, err
	}
	return u, nil
}

// UpdateUser changes username and/or bio for an account. Pass nil for fields
// to leave untouched. A duplicate username yields ErrUserExists.
func (s *Store) UpdateUser(id string, username, bio *string) (User, error) {
	if !validID(id) {
		return User{}, ErrUnauthorized
	}
	var sets []string
	var args []any
	if username != nil {
		name := strings.TrimSpace(*username)
		if !validUsername(name) {
			return User{}, fmt.Errorf("username must be %d-%d characters (letters, digits, _ or -)", usernameMinLen, usernameMaxLen)
		}
		sets = append(sets, "username = ?")
		args = append(args, name)
	}
	if bio != nil {
		b := strings.TrimSpace(*bio)
		if len(b) > bioMaxLen {
			return User{}, fmt.Errorf("bio must be <= %d characters", bioMaxLen)
		}
		sets = append(sets, "bio = ?")
		args = append(args, b)
	}
	if len(sets) == 0 {
		return s.GetUser(id)
	}
	args = append(args, id)
	if _, err := s.db.Exec(`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrUserExists
		}
		return User{}, err
	}
	return s.GetUser(id)
}

// ChangePassword verifies currentPassword and replaces the hash with one for
// newPassword. A wrong current password yields ErrInvalidLogin.
func (s *Store) ChangePassword(id, currentPassword, newPassword string) error {
	u, err := s.GetUser(id)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(currentPassword)) != nil {
		return ErrInvalidLogin
	}
	if len(newPassword) < passwordMinLen || len(newPassword) > passwordMaxLen {
		return fmt.Errorf("password must be %d-%d characters", passwordMinLen, passwordMaxLen)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
	return err
}

// Authenticate verifies a username/password pair, returning a generic error
// on any failure so callers cannot distinguish unknown users from bad passwords.
func (s *Store) Authenticate(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	var u User
	var created string
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, bio, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Bio, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Spend roughly the same time as a real compare to avoid user-enumeration timing.
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$XhVnI6wpdOu.XMWLnf5qPudexx2ojdpxnOq5eGOBnNFnO7Exq.5WG"), []byte(password))
			return User{}, ErrInvalidLogin
		}
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return User{}, ErrInvalidLogin
	}
	if u.CreatedAt, err = parseTime(created); err != nil {
		return User{}, err
	}
	return u, nil
}

// ── Sessions ───────────────────────────────────────────────────
// Sessions are opaque random tokens. Only a SHA-256 hash is stored, so a
// database leak does not hand an attacker usable sessions.

func newSessionToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession issues a new session for userID and returns the raw token
// (to be placed in the cookie) plus its expiry.
func (s *Store) CreateSession(userID string) (string, time.Time, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(sessionDuration)
	_, err = s.db.Exec(
		`INSERT INTO sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hashToken(token), userID, formatTime(now), formatTime(expires),
	)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// UserForSession resolves a raw session token to its (non-expired) user.
func (s *Store) UserForSession(token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthorized
	}
	var u User
	var created string
	err := s.db.QueryRow(`
		SELECT u.id, u.username, u.bio, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ?`,
		hashToken(token), formatTime(time.Now().UTC()),
	).Scan(&u.ID, &u.Username, &u.Bio, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUnauthorized
		}
		return User{}, err
	}
	if u.CreatedAt, err = parseTime(created); err != nil {
		return User{}, err
	}
	return u, nil
}

// DeleteSession revokes a single session (logout).
func (s *Store) DeleteSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, hashToken(token))
	return err
}

// DeleteExpiredSessions prunes sessions past their expiry. Call periodically.
func (s *Store) DeleteExpiredSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, formatTime(time.Now().UTC()))
	return err
}
