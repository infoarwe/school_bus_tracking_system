# Security review (S9-04), 2026-10-10

Scope: Go API, React admin web, deployment files (`deploy/`). Not in scope: the Android apps (mobile team).
Method: code reading of auth, tenant scoping, uploads, config and the web client; `govulncheck` and `npm audit`;
the isolation suite (`isolation_test.go`); a local run of the Docker stack behind Nginx.

## Already in good shape

- **Tenant isolation and RBAC:** every route is covered by the S9-02 suite (all roles × cross-school, IDs in
  paths and bodies, WebSocket subscriptions, no writes to the other school).
- **Tokens:** HS256 only, issuer and expiry required, MFA tokens cannot be used as access tokens; the session,
  user and school are re-checked in the DB on every request and every minute on WebSockets; refresh tokens are
  random, stored hashed and rotated.
- **Passwords:** bcrypt cost 12, constant-time path for unknown accounts, per-account and per-IP rate limits.
- **SQL:** parameterised everywhere; the only string-built SQL uses constant column names and integers.
- **Input:** JSON bodies capped at 1 MB, CSV import capped, logo uploads checked by content and re-encoded,
  announcement links limited to http(s), CSV exports formula-injection safe.
- **Errors:** internal errors return a generic message; details go to the log with the request ID.
- **Secrets:** stored third-party keys AES-GCM encrypted, never returned or audited; production refuses dev OTP,
  disabled 2FA and the zero encryption key.

## Fixed in this review

| # | Finding | Risk | Fix | Test |
|---|---|---|---|---|
| 1 | Go 1.26.5 standard library: 16 reachable advisories (HTTP/2 DoS, multipart memory limit, TLS, `os.Root` on Windows, …) | High (DoS) | `toolchain go1.26.9` in `go.mod`; Docker build image `golang:1.26.9-alpine`. govulncheck: 0 reachable | `govulncheck ./...` |
| 2 | Passwords over 72 bytes made bcrypt fail: user create / password reset answered 500 | Low | `auth.PasswordProblem` (8 characters to 72 bytes) used by the API and the bootstrap CLI → 400 | `TestSecurityReviewFixes` |
| 3 | Ending a session (`DELETE /auth/sessions/{id}`) was not audited | Low | Audited as `auth.session_revoke` | `TestSecurityReviewFixes` |
| 4 | Production started with the public placeholder `JWT_SECRET` from `.env.example`, or `CORS_ORIGINS=*` | High if misconfigured | Refused at start-up in production | `TestValidateForAPIProduction` |
| 5 | `/docs` loaded Swagger UI from a CDN with a floating version and no integrity check, on the same origin as the admin web's stored tokens | Medium (supply chain) | Pinned `swagger-ui-dist@5.33.0` with SRI hashes | `TestDocsPageCDNPinned` |
| 6 | No Content-Security-Policy on the admin web | Medium (XSS impact) | Enforced: `object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'`. Full policy (scripts, images, connections) sent as **report-only**, see open item A | Checked in the local stack |
| 7 | No way to create the first Super Admin in production | Blocker | `cmd/admin create-super-admin` (hidden password prompt, bootstrap only, audited) | `TestCreateFirstSuperAdmin` |

`npm audit`: 0 vulnerabilities.

To upgrade Swagger UI: pick a version at least two weeks old, then for each file
`curl -sL https://cdn.jsdelivr.net/npm/swagger-ui-dist@<v>/<file> | openssl dgst -sha384 -binary | openssl base64 -A`
and update the version and both `integrity` values in `internal/handlers/docs.go`.

## Not fixed (open items)

| # | Item | Risk | Why not now / what to do |
|---|---|---|---|
| A | Enforce the full CSP (scripts, images, connections) | Medium | Google Maps needs extra hosts, and the policy could not be tested in a browser here. Open the admin web with a Google Maps school, check the console for `Content-Security-Policy-Report-Only` violations, adjust, then move the policy to the enforced header in `deploy/nginx/templates/sbts.conf.template`. |
| B | Admin access and refresh tokens are in `localStorage` | Medium | Any XSS could read them. React escapes output and there is no raw-HTML rendering, which limits this. Proper fix: refresh token in an HttpOnly `SameSite=Strict` cookie and access token in memory. This changes the auth flow (CSRF handling), so it needs its own task. |
| C | No refresh-token reuse detection | Medium-low | A stolen refresh token can be used once; the real user is then logged out on their next refresh. Fix: token families, revoke the whole family when an old token is reused. |
| D | Failed logins are rate limited but not written to the audit log | Low | Add `auth.login_failed` (without the password) for monitoring when alerting is set up (S9-06). |
| E | OTP codes are stored as an unkeyed SHA-256 of mobile + code | Low | Codes expire in 5 minutes and are single-use. With a DB leak an active code could be brute-forced. Fix: HMAC with a server key. |
| F | A TOTP code can be reused within its 30-second window | Low | Track the last accepted time step per user. |
| G | Rate limiting allows requests when Redis is down | Low | Deliberate (availability). Nginx still limits per IP. |
| H | The API sets no read/write timeouts beyond `ReadHeaderTimeout` (WebSockets are long-lived) | Low | Only Nginx can reach the API, and it applies its own timeouts. Keep port 8085 unpublished. |
| I | `golang.org/x/crypto/openpgp` advisory (module required, package not used) | None | No fix exists; our code never calls it. |
| J | `golangci-lint` and `go test -race` not run on this Windows machine | — | CI runs both. Check the CI result before merging. |
