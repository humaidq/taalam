# Taalam

A learning management system proof-of-concept built on a custom blockchain.

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
