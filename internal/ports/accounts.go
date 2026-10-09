package ports

import (
	"context"
	"time"

	"playword/internal/domain/account"
)

// AccountStore persists users, sessions and rhyme books. Lookups that find
// nothing return account.ErrNotFound; a duplicate email account.ErrEmailTaken.
type AccountStore interface {
	// CreateUser makes an account; an empty passwordHash means sign-in only
	// through a provider, and verified marks the email as proven.
	CreateUser(ctx context.Context, email, passwordHash, displayName string, verified bool) (*account.User, error)
	UserByEmail(ctx context.Context, email string) (u *account.User, passwordHash string, err error)
	UserByID(ctx context.Context, id string) (*account.User, error)
	RecordLoginFailure(ctx context.Context, userID string) (failed int, lockedUntil time.Time, err error)
	ResetLoginFailures(ctx context.Context, userID string) error
	UpdateSettings(ctx context.Context, u *account.User) error
	DeleteUser(ctx context.Context, userID string) error
	SetPasswordHash(ctx context.Context, userID, hash string) error // "" disables password sign-in
	MarkEmailVerified(ctx context.Context, userID string) error

	UserIDByIdentity(ctx context.Context, provider, subject string) (string, error)
	LinkIdentity(ctx context.Context, userID string, id account.Identity) error
	Identities(ctx context.Context, userID string) ([]string, error)
	// DeleteUserSessions signs a user out everywhere except the keep session.
	DeleteUserSessions(ctx context.Context, userID, keepTokenID string) (int64, error)

	CreateSession(ctx context.Context, tokenID, userID string, expires time.Time, ipHash, userAgent string) error
	SessionUser(ctx context.Context, tokenID string) (*account.User, time.Time, error)
	DeleteSession(ctx context.Context, tokenID string) error

	CreateBook(ctx context.Context, b *account.Book) error
	Books(ctx context.Context, userID string) ([]account.Book, error)
	Book(ctx context.Context, id string) (*account.Book, error)
	UpdateBook(ctx context.Context, b *account.Book) error
	DeleteBook(ctx context.Context, id, userID string) error

	Items(ctx context.Context, bookID string) ([]account.Item, error)
	AddItem(ctx context.Context, it *account.Item) error
	HasEntryItem(ctx context.Context, bookID string, entryID int64) (bool, error)
	UpdateItemNote(ctx context.Context, bookID string, itemID int64, note string) error
	DeleteItem(ctx context.Context, bookID string, itemID int64) error
	ReorderItems(ctx context.Context, bookID string, ids []int64) error
	EntryExists(ctx context.Context, id int64) (bool, error)
}

// OAuthProvider signs users in with an external account (Google first),
// using the authorization-code flow with PKCE.
type OAuthProvider interface {
	Name() string
	// AuthURL is where to send the browser; verifier is the PKCE secret kept
	// by the server until the callback.
	AuthURL(state, verifier string) string
	// Exchange trades the callback code for the identity the provider vouches for.
	Exchange(ctx context.Context, code, verifier string) (account.Identity, error)
}
