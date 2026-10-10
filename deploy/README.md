# Deployment (shared dev, staging, production)

One Linux server runs everything with Docker Compose:

```
Internet ──443/80──▶ nginx (TLS, admin web, proxy) ──▶ api (Go) ──▶ postgres
                                                        └──────────▶ redis
```

| Service | Image | Published | Data |
|---|---|---|---|
| `nginx` | built from `deploy/nginx/Dockerfile` (admin web + config) | 80, 443 | – |
| `api` | built from `backend/Dockerfile` (api, migrate, seed, admin) | no (internal 8085) | volume `sbts_uploads` (school logos) |
| `migrate` | same image, runs `migrate up` and exits before `api` starts | no | – |
| `postgres` | `postgres:17-alpine` | no | volume `sbts_pgdata` |
| `redis` | `redis:7-alpine`, password protected | no | volume `sbts_redisdata` (live data only) |

The same files serve every environment; only `deploy/.env` differs.

> **Status:** the stack has been built and tested locally with Docker (HTTPS on a test certificate, WebSocket,
> OTP login, logo upload, backup and restore). It has **not** been deployed to a server yet: that needs the server
> details listed in "What we need" below.

## What we need before the first deploy

1. **A Linux server** (Ubuntu 24.04 LTS or Debian 12 recommended) with a public IP and SSH access by key.
   Size for the shared dev server: 2 vCPU, 4 GB RAM, 40 GB SSD. Production: size after the load test (S9-03).
2. **A DNS name** for it, e.g. `sbts-dev.<your-domain>`, with an A (and AAAA, if IPv6) record pointing at the server.
3. **An email address** for Let's Encrypt expiry notices.

Nobody should invent or share these over chat: the person who owns the server does the steps below.

## 1. Prepare the server (once)

```sh
# Docker Engine + Compose plugin (official instructions: https://docs.docker.com/engine/install/)
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"    # log out and in again

# Firewall: SSH, HTTP (certificate renewals + redirect), HTTPS. Nothing else.
sudo ufw allow OpenSSH && sudo ufw allow 80/tcp && sudo ufw allow 443/tcp && sudo ufw enable

sudo apt-get install -y certbot git
sudo mkdir -p /var/www/certbot /opt/sbts && sudo chown "$USER" /opt/sbts
```

Docker publishes ports past `ufw`; that is fine here because only Nginx publishes any.

## 2. Get the code and configure

```sh
git clone <repository-url> /opt/sbts
cd /opt/sbts/deploy
cp .env.example .env
chmod 600 .env
```

Edit `deploy/.env`. Generate every secret **on the server** (`openssl rand -hex 32`) and never reuse the
development values:

| Variable | Shared dev server | Production |
|---|---|---|
| `SERVER_NAME` | your dev hostname | your production hostname |
| `APP_ENV` | `staging` | `production` |
| `POSTGRES_PASSWORD`, `REDIS_PASSWORD` | `openssl rand -hex 24` | same |
| `JWT_SECRET` | `openssl rand -hex 32` | same (different value) |
| `DATA_ENCRYPTION_KEY` | `openssl rand -hex 32` | same (different value); **never change once data exists** |
| `OTP_DEV_CODE` | `123456` (mobile team logs in without SMS) | **empty** (the API refuses to start otherwise) |
| `REQUIRE_SUPER_ADMIN_2FA` | `true` | `true` (forced) |

Optional: a server-wide Firebase key for schools that have not uploaded their own:
`mkdir -p secrets && cp firebase-service-account.json secrets/ && chmod 644 secrets/*.json` (the API runs as
uid 65532 and must be able to read it). Without it those schools' pushes are only logged.

## 3. HTTPS certificate (once)

Nginx needs the certificate before it can start. Port 80 is still free, so use certbot's standalone mode:

```sh
sudo certbot certonly --standalone -d "$SERVER_NAME" -m you@example.com --agree-tos -n
```

Renewal runs from the systemd timer certbot installs. Once Nginx is running (step 4), switch renewals to the
webroot Nginx serves and reload Nginx after each renewal (certbot 2.3+; `reconfigure` does a test renewal first):

```sh
sudo certbot reconfigure --cert-name "$SERVER_NAME" --webroot -w /var/www/certbot \
  --deploy-hook "docker compose -f /opt/sbts/deploy/docker-compose.yml exec nginx nginx -s reload"
sudo certbot renew --dry-run
```

## 4. Start

```sh
cd /opt/sbts/deploy
docker compose build
docker compose up -d --wait
docker compose ps            # migrate: Exited (0); api, postgres, redis: healthy; nginx: up
curl https://$SERVER_NAME/health
# {"data":{"status":"ok","checks":{"postgres":"up","redis":"up"}}}
```

**Shared dev server only:** load the demo school and test accounts (refused when `APP_ENV=production`):

```sh
docker compose run --rm --no-deps api /app/seed
```

Then **change the seeded passwords** in the admin web: the defaults are in the public README. Never put real
student or parent data on the dev server: with `OTP_DEV_CODE` set, anyone who knows a registered mobile number
can log in to the apps.

**Production:** never run the demo seed. Create the first Super Admin instead (any environment; refused if a
Super Admin already exists or the email is in use; audited):

```sh
docker compose run --rm -it --no-deps api /app/admin create-super-admin --email owner@your-school.org --name "Owner Name"
```

Type the password at the hidden prompt (twice, at least 8 characters; never pass it as an argument). Then log in
at `https://<SERVER_NAME>` and set up an authenticator app when asked (2FA is always required in production).
Super Admins create schools and School Admins from the admin web.

## 5. Hand over to the mobile team (S2-13)

Send them, over a private channel:

- API base URL: `https://<SERVER_NAME>/api/v1`, WebSocket: `wss://<SERVER_NAME>/api/v1/ws`
- API docs: `https://<SERVER_NAME>/docs` (machine-readable: `/openapi.yaml`)
- Test driver and parent mobile numbers (from the seed output) and the dev OTP code

S2-13 is done when they call `GET /health` from a phone over mobile data and get `"status":"ok"`.

## Updating

```sh
cd /opt/sbts && git pull
cd deploy && docker compose build && docker compose up -d --wait
```

`migrate` runs again before the new API starts. Migrations only move forward; take a backup first (below).
To roll back code, check out the previous commit and rebuild. A migration that already ran is **not** undone by
that: restore the backup taken before the update, or ask a developer for a `migrate down`.

Tag images to keep the previous build around: `SBTS_VERSION=2026-10-10 docker compose build`.

## Logs

```sh
docker compose logs -f api      # JSON logs in production; every line has request_id
docker compose logs -f nginx    # access log without query strings (no WebSocket tokens)
```

Docker keeps logs in its default json-file driver. Limit their size in `/etc/docker/daemon.json`:
`{"log-driver":"json-file","log-opts":{"max-size":"50m","max-file":"5"}}`, then `sudo systemctl restart docker`.

## Backups

What must be backed up, and how:

| What | Where | How |
|---|---|---|
| Database | volume `sbts_pgdata` | `pg_dump` by `scripts/backup.sh` |
| School logos (uploads) | volume `sbts_uploads` | tar by `scripts/backup.sh` |
| `deploy/.env` (holds `DATA_ENCRYPTION_KEY`, passwords) | server disk | **by hand**, into a password manager or secret store. Without `DATA_ENCRYPTION_KEY` the stored Google Maps and Firebase keys cannot be decrypted. |
| `deploy/secrets/` (optional Firebase key) | server disk | by hand, like `.env` |
| Redis | volume `sbts_redisdata` | not needed: live positions and counters rebuild themselves |

Daily backup with 14 days kept (`KEEP_DAYS` to change):

```sh
sudo mkdir -p /var/backups/sbts && sudo chown "$USER" /var/backups/sbts
sh /opt/sbts/deploy/scripts/backup.sh /var/backups/sbts
# cron: 15 2 * * *  sh /opt/sbts/deploy/scripts/backup.sh /var/backups/sbts >> /var/log/sbts-backup.log 2>&1
```

**Copy the backup folder off the server** every day (another machine or object storage, e.g. `rclone` or
`rsync`). A backup on the same disk is lost with the server. Test a restore at least once a month.

### Restore

Stop the API first so nothing writes during the restore:

```sh
cd /opt/sbts/deploy
docker compose stop nginx api

# Database: replace the current one with the dump.
docker compose exec -T postgres psql -U sbts -d postgres -c "DROP DATABASE sbts WITH (FORCE)" -c "CREATE DATABASE sbts OWNER sbts"
docker compose exec -T postgres pg_restore -U sbts -d sbts --no-owner < /var/backups/sbts/db-YYYYMMDDTHHMMSSZ.dump

# Uploads: replace the volume's contents with the archive.
docker run --rm -v sbts_uploads:/data -v /var/backups/sbts:/backup:ro alpine:3.22 \
  sh -c 'rm -rf /data/* && tar -xzf /backup/uploads-YYYYMMDDTHHMMSSZ.tar.gz -C /data && chown -R 65532:65532 /data'

docker compose up -d --wait
```

Restore `deploy/.env` too when moving to a new server: the database is only readable with the same
`DATA_ENCRYPTION_KEY`.

## Load test (S9-03)

`backend/cmd/loadtest` sends GPS from N buses every 5 s while M parents watch their child's trip over the
WebSocket, and reports upload and delivery latency (target: GPS upload p95 < 300 ms). It **creates data** in the
admin's school, so run it only on a throwaway stack, never on the shared dev server or production:

```sh
# deploy/.env for the throwaway stack: APP_ENV=staging, OTP_DEV_CODE=123456, and (all traffic comes from one IP)
#   RATE_LIMIT_IP_PER_MINUTE / _USER_PER_MINUTE / _AUTH_PER_15_MINUTES / _OTP_SEND_PER_HOUR = 1000000
docker compose -p sbts-load up -d --wait && docker compose -p sbts-load run --rm --no-deps api /app/seed
cd ../backend
go run ./cmd/loadtest -api https://<host> -buses 200 -parents 1000 -duration 3m -yes   # -insecure for a self-signed cert
cd ../deploy && docker compose -p sbts-load down -v      # remove everything it created
```

Nginx still limits each IP to 30 requests/s, so set-up waits and retries; the measured phase counts any 429 as an
error. Run it from a second machine to include the network.

## Security notes

- Only Nginx is reachable from outside. The API trusts `X-Real-IP` / `X-Forwarded-For`
  (`TRUST_PROXY_HEADERS=true`) **only because** Nginx overwrites them and the API port is not published. Never
  publish port 8085.
- Rate limits: Nginx allows 30 requests/s per IP (burst 60) on `/api/`; the API adds per-IP, per-user, login and
  OTP limits (see `/docs`, "Rate limits").
- Content-Security-Policy: a safe subset is enforced; the full policy is report-only until checked in a browser
  with Google Maps (`docs/SECURITY_REVIEW.md`, item A).
- Security review findings and open items: `docs/SECURITY_REVIEW.md`.
- TLS 1.2/1.3 only, HSTS on. Keep the server patched (`unattended-upgrades`) and rebuild images monthly to pick up
  base image fixes: `docker compose build --pull && docker compose up -d --wait`.
- `/docs` is public on purpose (mobile team). Production can restrict it later if wanted.
- Monitoring (Prometheus + Grafana) is not part of this setup yet (S9-06). Until then, watch `/health` with an
  external uptime checker.
