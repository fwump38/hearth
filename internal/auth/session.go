// Package auth provides users, server-side sessions and OIDC login.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// User is a logged-in person.
type User struct {
	ID    int64
	Sub   string
	Email string
	Name  string
}

// Display returns a friendly name.
func (u *User) Display() string {
	switch {
	case u.Name != "":
		return u.Name
	case u.Email != "":
		return u.Email
	}
	return u.Sub
}

type ctxKey struct{}

// WithUser stores the user on the context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// FromContext returns the current user or nil.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// Store persists users and sessions in SQLite.
type Store struct{ DB *sql.DB }

// RandomToken returns n random bytes as hex.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Upsert creates or updates a user by OIDC subject. created reports first login.
func (s Store) Upsert(sub, email, name string) (u *User, created bool, err error) {
	u = &User{Sub: sub, Email: email, Name: name}
	err = s.DB.QueryRow(`SELECT id FROM users WHERE sub=?`, sub).Scan(&u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := s.DB.Exec(`INSERT INTO users(sub,email,name) VALUES(?,?,?)`, sub, email, name)
		if err != nil {
			return nil, false, err
		}
		u.ID, _ = res.LastInsertId()
		return u, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	_, err = s.DB.Exec(`UPDATE users SET email=?, name=? WHERE id=?`, email, name, u.ID)
	return u, false, err
}

// CreateSession returns a new session id.
func (s Store) CreateSession(userID int64, idToken string, ttl time.Duration) (string, error) {
	id := RandomToken(32)
	_, err := s.DB.Exec(`INSERT INTO sessions(id,user_id,expires_at,id_token) VALUES(?,?,?,?)`,
		id, userID, time.Now().Add(ttl).Unix(), idToken)
	return id, err
}

// Lookup resolves a session id to its user and ID token.
func (s Store) Lookup(id string) (*User, string, error) {
	u := &User{}
	var exp int64
	var tok string
	err := s.DB.QueryRow(`SELECT u.id,u.sub,u.email,u.name,s.expires_at,s.id_token
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.id=?`, id).
		Scan(&u.ID, &u.Sub, &u.Email, &u.Name, &exp, &tok)
	if err != nil {
		return nil, "", err
	}
	if time.Now().Unix() > exp {
		s.DeleteSession(id)
		return nil, "", sql.ErrNoRows
	}
	return u, tok, nil
}

// DeleteSession removes a session.
func (s Store) DeleteSession(id string) { s.DB.Exec(`DELETE FROM sessions WHERE id=?`, id) }

// Purge removes expired sessions.
func (s Store) Purge() { s.DB.Exec(`DELETE FROM sessions WHERE expires_at<?`, time.Now().Unix()) }
