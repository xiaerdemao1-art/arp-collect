# Contributing to arp-collect

Thanks for your interest in contributing! This project is small and informal —
issues and pull requests are the way to go.

## Reporting Bugs

Open an issue with:

- What you did, what you expected, what happened
- `arp_server -version` / `arp_collector -version` output
- Relevant config (redact secrets!) and logs
- Device vendor/model/firmware if SNMP-related

## Pull Requests

1. Fork the repo and create a branch from `main`.
2. Make your change. Keep it focused — one concern per PR.
3. Before submitting:

   ```bash
   gofmt -l .          # must be empty
   go vet ./...
   go build ./...
   go test ./...
   ```

4. If you touched the frontend (`web/src`), rebuild the embedded assets:

   ```bash
   cd web && npm ci && npm run build
   ```

   and commit the updated `internal/server/web/dist` (it is intentionally
   tracked so users can build without Node.js).

5. Write a clear commit message describing the *why*.

## Guidelines

- **Never commit secrets.** Real credentials belong in `config/config.yaml`
  (git-ignored) or environment variables. Config examples use
  `192.0.2.0/24` (TEST-NET-1) IPs and `*.example.com` hostnames.
- Follow existing code style (standard Go, zap for logging, Chinese comments
  are fine in this codebase — match surrounding style).
- Update `CHANGELOG.md` under "Unreleased" for user-facing changes.
- Discussion first for large changes: open an issue before investing in a
  big refactor or a new feature (e.g. SNMPv3, HTTPS support).

## License

By contributing, you agree that your contributions will be licensed under the
[Apache License 2.0](LICENSE).
