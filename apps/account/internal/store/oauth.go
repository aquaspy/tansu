package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// AuthCodeTTL bounds code interception: 60 seconds, single use.
	AuthCodeTTL = time.Minute
	// AccessTokenTTL bounds userinfo calls: 5 minutes, then the app
	// relies on its own local session.
	AccessTokenTTL = 5 * time.Minute
)

// Client is one registered suite app.
type Client struct {
	ID           string
	SecretDigest string
	Name         string
	Home         string
	Icon         string
	RedirectURIs []string
}

// AllowsRedirect matches redirect_uri exactly against the allowlist
// (no prefix matching, no wildcards — fail closed).
func (c *Client) AllowsRedirect(uri string) bool {
	uri = strings.TrimSpace(uri)
	for _, allowed := range c.RedirectURIs {
		if uri != "" && uri == allowed {
			return true
		}
	}
	return false
}

func randomToken(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func digestToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// SeedClients upserts the static registry: secrets are bcrypt-hashed,
// and re-seeding with identical values is a no-op (no re-hash, no
// churn on linked apps).
func (s *Store) SeedClients(clients []SeedClient) error {
	for _, c := range clients {
		uris, _ := json.Marshal(c.RedirectURIs)
		var existing Client
		var urisRaw, digest string
		err := s.db.QueryRow(`SELECT id, secret_digest, name, home, icon, redirect_uris
			FROM oauth_clients WHERE id = ?`, c.ID).
			Scan(&existing.ID, &digest, &existing.Name, &existing.Home, &existing.Icon, &urisRaw)
		if err == nil {
			existing.SecretDigest = digest
			if bcrypt.CompareHashAndPassword([]byte(digest), []byte(c.Secret)) == nil &&
				existing.Name == c.Name && existing.Home == c.Home &&
				existing.Icon == c.Icon && urisRaw == string(uris) {
				continue
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(c.Secret), bcrypt.MinCost)
		if err != nil {
			return err
		}
		ts := now()
		_, err = s.db.Exec(`INSERT INTO oauth_clients
			(id, secret_digest, name, home, icon, redirect_uris, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET secret_digest = excluded.secret_digest,
				name = excluded.name, home = excluded.home, icon = excluded.icon,
				redirect_uris = excluded.redirect_uris, updated_at = excluded.updated_at`,
			c.ID, string(hash), c.Name, c.Home, c.Icon, string(uris), ts, ts)
		if err != nil {
			return err
		}
	}
	// Drop clients removed from the registry; cascades clean their
	// codes, tokens, and links.
	if len(clients) == 0 {
		_, err := s.db.Exec(`DELETE FROM oauth_clients`)
		return err
	}
	args := make([]any, 0, len(clients))
	placeholders := make([]string, 0, len(clients))
	for _, c := range clients {
		args = append(args, c.ID)
		placeholders = append(placeholders, "?")
	}
	_, err := s.db.Exec(`DELETE FROM oauth_clients WHERE id NOT IN (`+
		strings.Join(placeholders, ", ")+`)`, args...)
	return err
}

// SeedClient is the boot-time registry row (secret in clear, hashed
// on the way in, never stored or logged).
type SeedClient struct {
	ID           string
	Secret       string
	Name         string
	Home         string
	Icon         string
	RedirectURIs []string
}

func scanClient(row interface{ Scan(...any) error }) (*Client, error) {
	c := &Client{}
	var urisRaw string
	err := row.Scan(&c.ID, &c.SecretDigest, &c.Name, &c.Home, &c.Icon, &urisRaw)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(urisRaw), &c.RedirectURIs)
	return c, nil
}

func (s *Store) FindClient(id string) (*Client, error) {
	c, err := scanClient(s.db.QueryRow(`SELECT id, secret_digest, name, home, icon, redirect_uris
		FROM oauth_clients WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// AuthenticateClient checks the client_secret_basic credential.
func (s *Store) AuthenticateClient(id, secret string) (*Client, error) {
	c, err := s.FindClient(id)
	if err != nil {
		return nil, ErrNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(c.SecretDigest), []byte(secret)) != nil {
		return nil, ErrNotFound
	}
	return c, nil
}

func (s *Store) ListClients() ([]*Client, error) {
	rows, err := s.db.Query(`SELECT id, secret_digest, name, home, icon, redirect_uris
		FROM oauth_clients ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AuthCode is a consumed-once grant bound to one client, redirect,
// and PKCE challenge.
type AuthCode struct {
	Digest      string
	ClientID    string
	UserID      int64
	RedirectURI string
	Challenge   string
	ExpiresAt   time.Time
}

// CreateAuthCode mints a code for a logged-in user. Only the digest
// is stored; the raw code travels in the redirect.
func (s *Store) CreateAuthCode(clientID string, userID int64, redirectURI, challenge string) (string, error) {
	raw, err := randomToken(32)
	if err != nil {
		return "", err
	}
	exp := formatTime(time.Now().UTC().Add(AuthCodeTTL))
	_, err = s.db.Exec(`INSERT INTO auth_codes
		(code_digest, client_id, user_id, redirect_uri, challenge, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		digestToken(raw), clientID, userID, redirectURI, challenge, exp, now())
	if err != nil {
		return "", err
	}
	return raw, nil
}

// RedeemAuthCode consumes a code exactly once: it must exist, be
// unexpired, and match the client + redirect. The row is deleted in
// the same transaction (replay fails closed).
func (s *Store) RedeemAuthCode(raw, clientID, redirectURI string) (*AuthCode, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	code := &AuthCode{}
	var expRaw string
	err = tx.QueryRow(`SELECT code_digest, client_id, user_id, redirect_uri, challenge, expires_at
		FROM auth_codes WHERE code_digest = ?`, digestToken(strings.TrimSpace(raw))).
		Scan(&code.Digest, &code.ClientID, &code.UserID, &code.RedirectURI, &code.Challenge, &expRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	code.ExpiresAt, _ = parseTime(expRaw)
	if code.ClientID != clientID || code.RedirectURI != redirectURI ||
		time.Now().UTC().After(code.ExpiresAt) {
		_, _ = tx.Exec(`DELETE FROM auth_codes WHERE code_digest = ?`, code.Digest)
		_ = tx.Commit()
		return nil, ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM auth_codes WHERE code_digest = ?`, code.Digest); err != nil {
		return nil, err
	}
	ts := now()
	if _, err := tx.Exec(`INSERT INTO account_links (user_id, client_id, linked_at)
		VALUES (?, ?, ?) ON CONFLICT(user_id, client_id) DO UPDATE SET linked_at = excluded.linked_at`,
		code.UserID, code.ClientID, ts); err != nil {
		return nil, err
	}
	return code, tx.Commit()
}

func (s *Store) CreateAccessToken(clientID string, userID int64) (string, error) {
	raw, err := randomToken(32)
	if err != nil {
		return "", err
	}
	exp := formatTime(time.Now().UTC().Add(AccessTokenTTL))
	_, err = s.db.Exec(`INSERT INTO access_tokens
		(token_digest, client_id, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)`, digestToken(raw), clientID, userID, exp, now())
	if err != nil {
		return "", err
	}
	return raw, nil
}

// FindAccessToken validates a userinfo bearer: digest match +
// unexpired. One query, fail closed.
func (s *Store) FindAccessToken(raw string) (userID int64, clientID string, err error) {
	var expRaw string
	qerr := s.db.QueryRow(`SELECT user_id, client_id, expires_at FROM access_tokens
		WHERE token_digest = ?`, digestToken(strings.TrimSpace(raw))).
		Scan(&userID, &clientID, &expRaw)
	if errors.Is(qerr, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	if qerr != nil {
		return 0, "", qerr
	}
	exp, _ := parseTime(expRaw)
	if time.Now().UTC().After(exp) {
		return 0, "", ErrNotFound
	}
	return userID, clientID, nil
}

// LinkedClientIDs reports the apps a user connected (hub tails).
func (s *Store) LinkedClientIDs(userID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT client_id FROM account_links
		WHERE user_id = ? ORDER BY linked_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DeleteStaleOAuth removes expired codes and tokens. Failures are
// ignored so sweeps never break a request.
func (s *Store) DeleteStaleOAuth() {
	cutoff := now()
	_, _ = s.db.Exec(`DELETE FROM auth_codes WHERE expires_at < ?`, cutoff)
	_, _ = s.db.Exec(`DELETE FROM access_tokens WHERE expires_at < ?`, cutoff)
	// Unconfirmed signups share this sweep. An expired row never becomes a user.
	_, _ = s.db.Exec(`DELETE FROM pending_signups WHERE expires_at < ?`, cutoff)
	_, _ = s.db.Exec(`DELETE FROM password_resets WHERE expires_at < ?`, cutoff)
}
