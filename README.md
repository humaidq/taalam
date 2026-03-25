# Taalam

Taalam is now trimmed down to the minimal app shell needed for the next phase of the project.

What remains:

- landing page
- passkey login and setup
- CSRF protection for forms and WebAuthn flows
- account management
- invite-based new user onboarding
- PostgreSQL-backed sessions and auth data
- Nix flake and devshell support for local development

Run locally from `src/`:

```bash
go run . migrate up --database-url "$DATABASE_URL"
go run . start --database-url "$DATABASE_URL" --port 8080
```

Useful environment variables:

```bash
export DATABASE_URL='postgres://user:pass@localhost:5432/taalam?sslmode=disable'
export WEBAUTHN_RP_ID='localhost'
export WEBAUTHN_RP_ORIGINS='http://localhost:8080'
export CSRF_SECRET='replace-with-a-random-secret'
export BOOTSTRAP_TOKEN='replace-with-a-random-secret'
```
