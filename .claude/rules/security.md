# Security and privacy (always)

- Secrets only from the environment (`.env` locally, git-ignored; platform secrets in production). Never
  print, log, commit or read `.env`; `.env.example` holds names only.
- Passwords: argon2id via `account.HashPassword`; session tokens stored as sha256; constant-time compares.
- OAuth: PKCE + single-use state cookie; only verified emails; linking to an existing password account clears
  the password and ends its sessions (pre-hijacking defence) — keep that.
- Privacy promises on `/privacy` are a contract: no IPs in events, hashed IPs in sessions, events kept 180
  days, lyrics never in analytics. If you store something new, update the privacy page in the same change.
- Validate and bound all input; escape output (templ does); no raw HTML from user data.
