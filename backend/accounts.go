package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
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
	IsAdmin      bool      `json:"isAdmin"`
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

// scanUser reads the standard account column list:
// id, username, password_hash, bio, is_admin, created_at.
func scanUser(row interface{ Scan(dest ...any) error }) (User, error) {
	var u User
	var created string
	var isAdmin int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Bio, &isAdmin, &created); err != nil {
		return User{}, err
	}
	u.IsAdmin = isAdmin != 0
	var err error
	if u.CreatedAt, err = parseTime(created); err != nil {
		return User{}, err
	}
	return u, nil
}

// GetUser returns the account with the given ID.
func (s *Store) GetUser(id string) (User, error) {
	if !validID(id) {
		return User{}, ErrUnauthorized
	}
	u, err := scanUser(s.db.QueryRow(
		`SELECT id, username, password_hash, bio, is_admin, created_at FROM users WHERE id = ?`, id,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUnauthorized
		}
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
	return scanUser(s.db.QueryRow(
		`SELECT id, username, password_hash, bio, is_admin, created_at FROM users WHERE username = ?`,
		username,
	))
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

// UpdateAccount atomically applies optional username/bio changes together with
// an optional password change. All input is validated before anything is
// written, so a rejected username or bio can never leave a changed password.
// When newPassword is non-empty, currentPassword must match; the password
// change revokes every existing session for the account. passwordChanged
// reports whether the hash was replaced so the caller can re-issue a session.
func (s *Store) UpdateAccount(id string, username, bio *string, currentPassword, newPassword string) (User, bool, error) {
	if !validID(id) {
		return User{}, false, ErrUnauthorized
	}
	var sets []string
	var args []any
	if username != nil {
		name := strings.TrimSpace(*username)
		if !validUsername(name) {
			return User{}, false, fmt.Errorf("username must be %d-%d characters (letters, digits, _ or -)", usernameMinLen, usernameMaxLen)
		}
		sets = append(sets, "username = ?")
		args = append(args, name)
	}
	if bio != nil {
		b := strings.TrimSpace(*bio)
		if len(b) > bioMaxLen {
			return User{}, false, fmt.Errorf("bio must be <= %d characters", bioMaxLen)
		}
		sets = append(sets, "bio = ?")
		args = append(args, b)
	}

	passwordChanged := newPassword != ""
	if passwordChanged {
		if len(newPassword) < passwordMinLen || len(newPassword) > passwordMaxLen {
			return User{}, false, fmt.Errorf("password must be %d-%d characters", passwordMinLen, passwordMaxLen)
		}
		current, err := s.GetUser(id)
		if err != nil {
			return User{}, false, err
		}
		if bcrypt.CompareHashAndPassword([]byte(current.PasswordHash), []byte(currentPassword)) != nil {
			return User{}, false, ErrInvalidLogin
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			return User{}, false, err
		}
		sets = append(sets, "password_hash = ?")
		args = append(args, string(hash))
	}

	if len(sets) == 0 {
		u, err := s.GetUser(id)
		return u, false, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return User{}, false, err
	}
	defer tx.Rollback()
	args = append(args, id)
	if _, err := tx.Exec(`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		if isUniqueViolation(err) {
			return User{}, false, ErrUserExists
		}
		return User{}, false, err
	}
	if passwordChanged {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			return User{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return User{}, false, err
	}
	u, err := s.GetUser(id)
	return u, passwordChanged, err
}

// ChangePassword verifies currentPassword and replaces the hash with one for
// newPassword. A wrong current password yields ErrInvalidLogin. All existing
// sessions are revoked so a stolen or stale session cannot survive the change.
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
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Authenticate verifies a username/password pair, returning a generic error
// on any failure so callers cannot distinguish unknown users from bad passwords.
func (s *Store) Authenticate(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	u, err := scanUser(s.db.QueryRow(
		`SELECT id, username, password_hash, bio, is_admin, created_at FROM users WHERE username = ?`,
		username,
	))
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
	u, err := scanUser(s.db.QueryRow(`
		SELECT u.id, u.username, u.password_hash, u.bio, u.is_admin, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ?`,
		hashToken(token), formatTime(time.Now().UTC()),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUnauthorized
		}
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

// ── Admin accounts ─────────────────────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// EnsureAdmin provisions the configured administrator account: it creates the
// account when missing, promotes an existing one, and resets the password
// whenever it differs from the configured value (the config is authoritative).
func (s *Store) EnsureAdmin(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return User{}, errors.New("admin username and password must both be set")
	}
	if err := validateCredentials(username, password); err != nil {
		return User{}, err
	}
	u, err := s.GetUserByUsername(username)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if u, err = s.CreateUser(username, password); err != nil {
			return User{}, err
		}
	case err != nil:
		return User{}, err
	default:
		// Provisioning an existing account must not let a session created
		// before provisioning inherit the reset password or admin flag.
		changed := false
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
			hash, herr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if herr != nil {
				return User{}, herr
			}
			if _, herr := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), u.ID); herr != nil {
				return User{}, herr
			}
			changed = true
		}
		if !u.IsAdmin {
			if err := s.SetAdmin(u.ID, true); err != nil {
				return User{}, err
			}
			u.IsAdmin = true
			changed = true
		}
		if changed {
			if _, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, u.ID); err != nil {
				return User{}, err
			}
		}
		return u, nil
	}
	if !u.IsAdmin {
		if err := s.SetAdmin(u.ID, true); err != nil {
			return User{}, err
		}
		u.IsAdmin = true
	}
	return u, nil
}

// SetAdmin flips the administrator flag on an account.
func (s *Store) SetAdmin(id string, admin bool) error {
	if !validID(id) {
		return ErrUnauthorized
	}
	_, err := s.db.Exec(`UPDATE users SET is_admin = ? WHERE id = ?`, boolToInt(admin), id)
	return err
}

// SetPassword replaces an account's password without checking the old one.
// Intended for administrator resets; callers must already be authorized.
// Existing sessions are revoked so the reset takes effect immediately.
func (s *Store) SetPassword(id, password string) error {
	if !validID(id) {
		return ErrUnauthorized
	}
	if len(password) < passwordMinLen || len(password) > passwordMaxLen {
		return fmt.Errorf("password must be %d-%d characters", passwordMinLen, passwordMaxLen)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ListUsers returns every account ordered case-insensitively by username.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(
		`SELECT id, username, password_hash, bio, is_admin, created_at FROM users ORDER BY username COLLATE NOCASE`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// DeleteUser removes an account and everything referencing it: its schematics
// (rows plus blobs), the feedback left on those schematics, its own ratings,
// likes and sessions.
func (s *Store) DeleteUser(id string) error {
	if !validID(id) {
		return ErrUnauthorized
	}
	u, err := s.GetUser(id)
	if err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id FROM schematics WHERE owner_id = ?`, u.ID)
	if err != nil {
		return err
	}
	var owned []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			rows.Close()
			return err
		}
		owned = append(owned, sid)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, sid := range owned {
		if err := os.Remove(s.FilePath(sid)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sid := range owned {
		if _, err := tx.Exec(`DELETE FROM ratings WHERE schematic_id = ?`, sid); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM likes WHERE schematic_id = ?`, sid); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM project_schematics WHERE schematic_id = ?`, sid); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`DELETE FROM project_schematics WHERE project_id IN (SELECT id FROM projects WHERE owner_id = ?)`,
		u.ID,
	); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM ratings WHERE user_id = ?`,
		`DELETE FROM likes WHERE user_id = ?`,
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM schematics WHERE owner_id = ?`,
		`DELETE FROM projects WHERE owner_id = ?`,
		`DELETE FROM users WHERE id = ?`,
	} {
		if _, err := tx.Exec(q, u.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
