package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"playword/internal/adapters/postgres/db"
	"playword/internal/domain/account"
	"playword/internal/ports"
)

var _ ports.AccountStore = (*Store)(nil)

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return account.ErrNotFound
	}
	return err
}

func ts(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func (s *Store) CreateUser(ctx context.Context, email, hash, name string, verified bool) (*account.User, error) {
	r, err := db.New(s.pool).CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: hash, DisplayName: nullable(name), Verified: verified})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, account.ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}
	return &account.User{ID: r.ID, Email: r.Email, DisplayName: r.DisplayName, DefaultDialect: r.DefaultDialect,
		DefaultLang: r.DefaultLang, DefaultLevel: r.DefaultLevel, Role: r.Role, CreatedAt: ts(r.CreatedAt), HasPassword: hash != ""}, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*account.User, string, error) {
	r, err := db.New(s.pool).GetUserByEmail(ctx, email)
	if err != nil {
		return nil, "", notFound(err)
	}
	return &account.User{ID: r.ID, Email: r.Email, DisplayName: r.DisplayName, DefaultDialect: r.DefaultDialect,
		DefaultLang: r.DefaultLang, DefaultLevel: r.DefaultLevel, Role: r.Role, FailedLogins: int(r.FailedLogins),
		LockedUntil: ts(r.LockedUntil), CreatedAt: ts(r.CreatedAt), HasPassword: r.PasswordHash != ""}, r.PasswordHash, nil
}

func (s *Store) UserByID(ctx context.Context, id string) (*account.User, error) {
	r, err := db.New(s.pool).GetUserByID(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	return &account.User{ID: r.ID, Email: r.Email, DisplayName: r.DisplayName, DefaultDialect: r.DefaultDialect,
		DefaultLang: r.DefaultLang, DefaultLevel: r.DefaultLevel, Role: r.Role, CreatedAt: ts(r.CreatedAt)}, nil
}

func (s *Store) RecordLoginFailure(ctx context.Context, userID string) (int, time.Time, error) {
	r, err := db.New(s.pool).RecordLoginFailure(ctx, db.RecordLoginFailureParams{ID: userID,
		MaxFailures: account.MaxFailures, LockFor: account.LockFor})
	if err != nil {
		return 0, time.Time{}, notFound(err)
	}
	return int(r.FailedLogins), ts(r.LockedUntil), nil
}

func (s *Store) ResetLoginFailures(ctx context.Context, userID string) error {
	return db.New(s.pool).ResetLoginFailures(ctx, userID)
}

func (s *Store) UpdateSettings(ctx context.Context, u *account.User) error {
	return db.New(s.pool).UpdateUserSettings(ctx, db.UpdateUserSettingsParams{ID: u.ID, DisplayName: u.DisplayName,
		DefaultDialect: u.DefaultDialect, DefaultLang: u.DefaultLang, DefaultLevel: u.DefaultLevel})
}

// DeleteUser anonymizes the user's events and deletes the account (cascading
// to sessions and books) in one transaction.
func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	var uid pgtype.UUID
	if err := uid.Scan(userID); err != nil {
		return account.ErrNotFound
	}
	if _, err := q.AnonymizeUserEvents(ctx, uid); err != nil {
		return err
	}
	if err := q.DeleteUser(ctx, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetRole grants a role by email (CLI: playword-cli user-role).
func (s *Store) SetRole(ctx context.Context, email, role string) error {
	return db.New(s.pool).SetUserRole(ctx, db.SetUserRoleParams{Email: email, Role: role})
}

func (s *Store) CreateSession(ctx context.Context, tokenID, userID string, expires time.Time, ipHash, ua string) error {
	return db.New(s.pool).CreateSession(ctx, db.CreateSessionParams{ID: tokenID, UserID: userID,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}, IpHash: nullable(ipHash), Ua: nullable(ua)})
}

func (s *Store) SessionUser(ctx context.Context, tokenID string) (*account.User, time.Time, error) {
	r, err := db.New(s.pool).GetSessionUser(ctx, tokenID)
	if err != nil {
		return nil, time.Time{}, notFound(err)
	}
	return &account.User{ID: r.ID, Email: r.Email, DisplayName: r.DisplayName, DefaultDialect: r.DefaultDialect,
		DefaultLang: r.DefaultLang, DefaultLevel: r.DefaultLevel, Role: r.Role, CreatedAt: ts(r.CreatedAt)}, ts(r.ExpiresAt), nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenID string) error {
	return db.New(s.pool).DeleteSession(ctx, tokenID)
}

// DeleteExpiredSessions removes sessions past their expiry (maintenance job).
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	return db.New(s.pool).DeleteExpiredSessions(ctx)
}

func (s *Store) CreateBook(ctx context.Context, b *account.Book) error {
	r, err := db.New(s.pool).CreateBook(ctx, db.CreateBookParams{UserID: b.UserID, Title: b.Title,
		Description: b.Description, IsPublic: b.IsPublic})
	if err != nil {
		return err
	}
	b.ID, b.CreatedAt, b.UpdatedAt = r.ID, ts(r.CreatedAt), ts(r.UpdatedAt)
	return nil
}

func bookFrom(id, userID, title, desc string, public bool, created, updated pgtype.Timestamptz, items int32) account.Book {
	return account.Book{ID: id, UserID: userID, Title: title, Description: desc, IsPublic: public,
		CreatedAt: ts(created), UpdatedAt: ts(updated), ItemCount: int(items)}
}

func (s *Store) Books(ctx context.Context, userID string) ([]account.Book, error) {
	rows, err := db.New(s.pool).ListBooks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]account.Book, len(rows))
	for i, r := range rows {
		out[i] = bookFrom(r.ID, r.UserID, r.Title, r.Description, r.IsPublic, r.CreatedAt, r.UpdatedAt, r.ItemCount)
	}
	return out, nil
}

func (s *Store) Book(ctx context.Context, id string) (*account.Book, error) {
	r, err := db.New(s.pool).GetBook(ctx, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" { // not a UUID
		return nil, account.ErrNotFound
	}
	if err != nil {
		return nil, notFound(err)
	}
	b := bookFrom(r.ID, r.UserID, r.Title, r.Description, r.IsPublic, r.CreatedAt, r.UpdatedAt, r.ItemCount)
	b.Lyrics = r.Lyrics
	return &b, nil
}

func (s *Store) SetLyrics(ctx context.Context, id, userID, lyrics string) error {
	return affected(db.New(s.pool).SetBookLyrics(ctx, db.SetBookLyricsParams{ID: id, UserID: userID, Lyrics: lyrics}))
}

func affected(n int64, err error) error {
	if err != nil {
		return err
	}
	if n == 0 {
		return account.ErrNotFound
	}
	return nil
}

func (s *Store) UpdateBook(ctx context.Context, b *account.Book) error {
	return affected(db.New(s.pool).UpdateBook(ctx, db.UpdateBookParams{ID: b.ID, UserID: b.UserID, Title: b.Title,
		Description: b.Description, IsPublic: b.IsPublic}))
}

func (s *Store) DeleteBook(ctx context.Context, id, userID string) error {
	return affected(db.New(s.pool).DeleteBook(ctx, db.DeleteBookParams{ID: id, UserID: userID}))
}

func (s *Store) Items(ctx context.Context, bookID string) ([]account.Item, error) {
	rows, err := db.New(s.pool).ListItems(ctx, bookID)
	if err != nil {
		return nil, err
	}
	out := make([]account.Item, len(rows))
	for i, r := range rows {
		out[i] = account.Item{ID: r.ID, BookID: r.BookID, EntryID: r.EntryID, Text: r.Text, Note: r.Note, Lang: r.Lang,
			Dialect: r.Dialect, Level: r.Level, MatchedFrom: r.MatchedFrom, Ord: int(r.Ord), CreatedAt: ts(r.CreatedAt)}
	}
	return out, nil
}

func (s *Store) AddItem(ctx context.Context, it *account.Item) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	custom := ""
	if it.EntryID == 0 {
		custom = it.Text
	}
	r, err := q.AddItem(ctx, db.AddItemParams{BookID: it.BookID, EntryID: it.EntryID, CustomText: custom, Note: it.Note,
		Lang: it.Lang, Dialect: it.Dialect, Level: it.Level, MatchedFrom: it.MatchedFrom})
	if err != nil {
		return err
	}
	if err := q.TouchBook(ctx, it.BookID); err != nil {
		return err
	}
	it.ID, it.Ord, it.CreatedAt = r.ID, int(r.Ord), ts(r.CreatedAt)
	return tx.Commit(ctx)
}

func (s *Store) HasEntryItem(ctx context.Context, bookID string, entryID int64) (bool, error) {
	return db.New(s.pool).HasEntryItem(ctx, db.HasEntryItemParams{BookID: bookID, EntryID: pgtype.Int8{Int64: entryID, Valid: true}})
}

func (s *Store) UpdateItemNote(ctx context.Context, bookID string, itemID int64, note string) error {
	return affected(db.New(s.pool).UpdateItemNote(ctx, db.UpdateItemNoteParams{ID: itemID, BookID: bookID, Note: note}))
}

func (s *Store) DeleteItem(ctx context.Context, bookID string, itemID int64) error {
	return affected(db.New(s.pool).DeleteItem(ctx, db.DeleteItemParams{ID: itemID, BookID: bookID}))
}

// ReorderItems sets ord = position for every id, in one transaction.
func (s *Store) ReorderItems(ctx context.Context, bookID string, ids []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	for i, id := range ids {
		if err := affected(q.SetItemOrd(ctx, db.SetItemOrdParams{ID: id, BookID: bookID, Ord: int32(i)})); err != nil {
			return fmt.Errorf("item %d: %w", id, err)
		}
	}
	if err := q.TouchBook(ctx, bookID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) EntryExists(ctx context.Context, id int64) (bool, error) {
	return db.New(s.pool).EntryExists(ctx, id)
}

func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string) error {
	return db.New(s.pool).SetPasswordHash(ctx, db.SetPasswordHashParams{ID: userID, PasswordHash: hash})
}

func (s *Store) MarkEmailVerified(ctx context.Context, userID string) error {
	return db.New(s.pool).MarkEmailVerified(ctx, userID)
}

func (s *Store) UserIDByIdentity(ctx context.Context, provider, subject string) (string, error) {
	id, err := db.New(s.pool).GetUserByIdentity(ctx, db.GetUserByIdentityParams{Provider: provider, Subject: subject})
	return id, notFound(err)
}

func (s *Store) LinkIdentity(ctx context.Context, userID string, id account.Identity) error {
	return db.New(s.pool).CreateIdentity(ctx, db.CreateIdentityParams{Provider: id.Provider, Subject: id.Subject,
		UserID: userID, Email: nullable(id.Email)})
}

func (s *Store) Identities(ctx context.Context, userID string) ([]string, error) {
	return db.New(s.pool).ListIdentities(ctx, userID)
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID, keepTokenID string) (int64, error) {
	return db.New(s.pool).DeleteUserSessions(ctx, db.DeleteUserSessionsParams{UserID: userID, Keep: keepTokenID})
}
