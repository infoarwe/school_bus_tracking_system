# backend/secrets

Secret files the API reads at start-up. **Everything in this folder except this README is git-ignored.**
Never commit, email or paste these files.

| File | What | Where to get it |
|---|---|---|
| `firebase-service-account.json` | Optional **fallback** Firebase key, used only for schools that have not uploaded their own (Settings → Push notifications in the admin web) | Firebase console → Project settings → Service accounts → *Generate new private key* |

The API looks for `secrets/firebase-service-account.json` (relative to `backend/`). To keep it elsewhere,
set `FCM_CREDENTIALS_FILE` to its path. Normally each school uploads its own key in the admin web (stored encrypted in the database), so this file is not needed.
If neither exists for a school, its push notifications are only written to the log.

On a server, put the file outside the code checkout (e.g. `/etc/sbts/firebase-service-account.json`),
readable only by the API's user (`chmod 600`), and point `FCM_CREDENTIALS_FILE` at it (or mount it into the container).
