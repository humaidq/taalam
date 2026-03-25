# AGENTS Guide for Taalam
This guide is for coding agents working in this repository.
Prefer precise, minimal changes that match existing patterns.

## Scope and Current State
- Main application code is in `src/`.
- This repository is intentionally narrowed to the web app shell, passkey auth, account management, invite onboarding, and PostgreSQL-backed persistence.
- Root Nix configuration is for development and packaging only.

## Repository Layout
- `src/main.go`: CLI entrypoint (`start`, `migrate`)
- `src/cmd/`: command wiring and startup/migration logic
- `src/db/`: DB pool, schema migration, auth/session models, invite helpers
- `src/routes/`: HTTP handlers, middleware, auth, onboarding, account pages
- `src/templates/`: server-rendered HTML templates (embedded)
- `src/static/`: CSS/static assets (embedded)
- `nix/`: devshell, formatting, checks

## Working Directories
- Run most Go commands from `src/` because `go.mod` is at `src/go.mod`.
- Run flake-wide Nix commands from repository root.

## Build Commands
### Go build/run (from `src/`)
```bash
go build ./...
go run . start --database-url "$DATABASE_URL" --port 8080
```
### DB migrations (from `src/`)
```bash
go run . migrate up --database-url "$DATABASE_URL"
go run . migrate down --database-url "$DATABASE_URL"
go run . migrate status --database-url "$DATABASE_URL"
go run . migrate version --database-url "$DATABASE_URL"
go run . migrate create add_new_table
```
### Nix commands (from repo root)
```bash
nix build .#taalam
nix develop
nix flake check
```

## Lint and Format Commands
### Preferred
```bash
nix fmt
nix flake check
```
### Direct
```bash
gofmt -w .
go vet ./...
golangci-lint run ./...
go test ./...
```

## Code Style Guidelines
- Keep existing copyright + SPDX header style.
- Always run `gofmt` on changed Go files.
- Prefer small, focused functions and early returns.
- Keep server-rendered HTML via Flamego templates.
- Use PRG flow for forms.
- Parameterize SQL and check `rows.Err()` after iteration.

## Things to Avoid
- Do not add platform-builder or deployment-management features unless the user requests them.
- Do not edit `src/vendor/` manually.

## Practical Agent Checklist Before Finishing
1. Run `gofmt -w` on changed Go files.
2. Run `go test ./...` from `src/`.
3. Run `nix build .#taalam` when Nix/package files changed.
