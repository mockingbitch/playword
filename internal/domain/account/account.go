// Package account is users, sessions and rhyme books (spec addendum, M5).
package account

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Errors the HTTP layer maps to status codes.
var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEmailTaken         = errors.New("email already registered")
	ErrBadCredentials     = errors.New("wrong email or password")
	ErrLocked             = errors.New("account temporarily locked")
	ErrUnauthenticated    = errors.New("not signed in")
	ErrForbidden          = errors.New("not allowed")
	ErrNotFound           = errors.New("not found")
	ErrTooManyGuestItems  = errors.New("guest notebook is full")
	ErrNotImplementedYet  = errors.New("not implemented yet")
	ErrSessionExpired     = errors.New("session expired")
	ErrEmailNotVerified   = errors.New("the provider has not verified this email")
	ErrPasswordTooShort   = fmt.Errorf("%w: password must have at least %d characters", ErrInvalidInput, MinPasswordLen)
	ErrPasswordTooLong    = fmt.Errorf("%w: password must have at most %d characters", ErrInvalidInput, MaxPasswordLen)
	errMalformedPassword  = errors.New("malformed password hash")
	errUnsupportedVariant = errors.New("unsupported password hash")
)

// Policy constants.
const (
	MinPasswordLen = 10
	MaxPasswordLen = 256
	MaxFailures    = 10               // consecutive failures before a lockout
	LockFor        = 15 * time.Minute // lockout length
	SessionTTL     = 30 * 24 * time.Hour
	GuestMaxItems  = 20
	MaxTitleLen    = 120
	MaxNoteLen     = 1000
)

// User is an account.
type User struct {
	ID             string
	Email          string
	DisplayName    string
	DefaultDialect string
	DefaultLang    string
	DefaultLevel   string
	Role           string // user | admin
	FailedLogins   int
	LockedUntil    time.Time
	CreatedAt      time.Time
	HasPassword    bool     // false for accounts created through a provider
	Providers      []string // linked sign-in providers ("google"); filled on demand
}

// Identity is an account at an external provider, as the provider vouches
// for it after sign-in.
type Identity struct {
	Provider      string // "google"
	Subject       string // the provider's stable id for the user
	Email         string
	EmailVerified bool
	Name          string
}

// IsAdmin reports the admin role.
func (u *User) IsAdmin() bool { return u != nil && u.Role == "admin" }

// Name is what the UI shows for the user.
func (u *User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	name, _, _ := strings.Cut(u.Email, "@")
	return name
}

// NormalizeEmail validates and lowercases an address.
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s, ".") {
		return "", fmt.Errorf("%w: email address", ErrInvalidInput)
	}
	return strings.ToLower(s), nil
}

// CheckPassword enforces the length policy (NIST 800-63B: length over
// composition rules).
func CheckPassword(p string) error {
	switch n := utf8.RuneCountInString(p); {
	case n < MinPasswordLen:
		return ErrPasswordTooShort
	case n > MaxPasswordLen:
		return ErrPasswordTooLong
	}
	return nil
}

// argon2id parameters: OWASP Password Storage Cheat Sheet (m=19 MiB, t=2, p=1).
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns a PHC-format argon2id hash.
func HashPassword(p string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(p), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks p against a hash from HashPassword, in constant time.
func VerifyPassword(p, hash string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errUnsupportedVariant
	}
	var version int
	var m, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errMalformedPassword
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &threads); err != nil {
		return false, errMalformedPassword
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false, errMalformedPassword
	}
	got := argon2.IDKey([]byte(p), salt, t, m, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// NewToken returns a random URL-safe token (32 bytes) for session and
// CSRF cookies.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// TokenID is what storage keeps for a session token: its SHA-256, so a
// leaked sessions table cannot be replayed (ADR-003 D7).
func TokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// HashIP keeps only a salted hash of a client address.
func HashIP(ip, salt string) string {
	sum := sha256.Sum256([]byte(salt + "|" + ip))
	return hex.EncodeToString(sum[:12])
}

// Book is a rhyme notebook.
type Book struct {
	ID          string
	UserID      string
	Title       string
	Description string
	IsPublic    bool
	Lyrics      string // the writing desk's text; filled by Book, not by Books
	ItemCount   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MaxLyrics bounds a book's lyrics, in characters.
const MaxLyrics = 20000

// Item is one saved rhyme, either a dictionary entry or the rapper's own text.
type Item struct {
	ID          int64
	BookID      string
	EntryID     int64  // 0 for custom text
	Text        string // the entry's headword, or the custom text
	Note        string
	Lang        string
	Dialect     string
	Level       string
	MatchedFrom string // what was searched when it was saved
	Ord         int
	CreatedAt   time.Time
}

// CleanTitle validates a book title.
func CleanTitle(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > MaxTitleLen {
		return "", fmt.Errorf("%w: title must have 1–%d characters", ErrInvalidInput, MaxTitleLen)
	}
	return s, nil
}

// CleanNote validates free text (descriptions, notes, custom items).
func CleanNote(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxNoteLen {
		return "", fmt.Errorf("%w: text must have at most %d characters", ErrInvalidInput, MaxNoteLen)
	}
	return s, nil
}

// GuestItem is a rhyme a signed-out visitor saved; kept in a cookie and
// imported into a book on sign-up (spec addendum §4).
type GuestItem struct {
	Text        string `json:"t"`
	EntryID     int64  `json:"e,omitempty"`
	Lang        string `json:"l,omitempty"`
	Dialect     string `json:"d,omitempty"`
	Level       string `json:"v,omitempty"`
	MatchedFrom string `json:"m,omitempty"`
}
