package account

import (
	"errors"
	"strings"
	"testing"
)

func TestEmailAndPassword(t *testing.T) {
	if e, err := NormalizeEmail("  MC@Example.VN "); err != nil || e != "mc@example.vn" {
		t.Errorf("%q %v", e, err)
	}
	for _, bad := range []string{"", "x", "a@b", "Name <a@b.vn>", "a b@c.vn"} {
		if _, err := NormalizeEmail(bad); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%q accepted", bad)
		}
	}
	if !errors.Is(CheckPassword("short"), ErrPasswordTooShort) || !errors.Is(CheckPassword(strings.Repeat("x", 257)), ErrPasswordTooLong) {
		t.Error("length policy")
	}
	if CheckPassword("mật khẩu đủ dài") != nil {
		t.Error("runes, not bytes")
	}
}

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("%q %v", h, err)
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Error("salts must differ")
	}
	if ok, err := VerifyPassword("correct horse battery", h); !ok || err != nil {
		t.Error("right password")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Error("wrong password")
	}
	for _, bad := range []string{"", "$bcrypt$x", "$argon2id$v=1$m=1,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=x$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=1,t=1,p=1$!!$aGFzaA", "$argon2id$v=19$m=1,t=1,p=1$c2FsdA$"} {
		if _, err := VerifyPassword("x", bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestTokensAndText(t *testing.T) {
	a, b := NewToken(), NewToken()
	if len(a) != 43 || a == b {
		t.Errorf("tokens %q %q", a, b)
	}
	if id := TokenID(a); id == a || len(id) != 64 || id != TokenID(strings.Clone(a)) {
		t.Error("TokenID is a stable sha256")
	}
	if HashIP("1.2.3.4", "s") == HashIP("1.2.3.4", "t") || len(HashIP("x", "s")) != 24 {
		t.Error("salted IP hash")
	}
	if _, err := CleanTitle(" "); !errors.Is(err, ErrInvalidInput) {
		t.Error("empty title")
	}
	if s, _ := CleanTitle("  Verse 1 "); s != "Verse 1" {
		t.Error("trim")
	}
	if _, err := CleanNote(strings.Repeat("x", MaxNoteLen+1)); !errors.Is(err, ErrInvalidInput) {
		t.Error("long note")
	}
	u := &User{Email: "mc@example.vn"}
	if u.Name() != "mc" || u.IsAdmin() {
		t.Error("name from email")
	}
	u.DisplayName, u.Role = "MC Phong", "admin"
	if u.Name() != "MC Phong" || !u.IsAdmin() || (*User)(nil).IsAdmin() {
		t.Error("display name / admin")
	}
}
