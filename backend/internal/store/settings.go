package store

import (
	"context"
	"time"
)

// MapsSettings holds a school's Google Maps keys. The server key stays encrypted here.
type MapsSettings struct {
	BrowserKey         string
	ServerKeyEncrypted []byte
}

func GetMapsSettings(ctx context.Context, q DBTX, schoolID string) (*MapsSettings, error) {
	var m MapsSettings
	err := q.QueryRow(ctx, `SELECT maps_browser_key, maps_server_key_enc FROM schools WHERE id = $1`, schoolID).
		Scan(&m.BrowserKey, &m.ServerKeyEncrypted)
	if err != nil {
		return nil, mapErr(err)
	}
	return &m, nil
}

// SetMapsSettings writes both keys; pass the current encrypted server key to keep it.
func SetMapsSettings(ctx context.Context, q DBTX, schoolID string, m MapsSettings) error {
	tag, err := q.Exec(ctx, `UPDATE schools SET maps_browser_key = $2, maps_server_key_enc = $3 WHERE id = $1`,
		schoolID, m.BrowserKey, m.ServerKeyEncrypted)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PushSettings is a school's Firebase (FCM) key. The JSON stays encrypted here.
type PushSettings struct {
	CredentialsEncrypted []byte
	ProjectID            string
	ClientEmail          string
	UpdatedAt            *time.Time
}

func GetPushSettings(ctx context.Context, q DBTX, schoolID string) (*PushSettings, error) {
	var p PushSettings
	err := q.QueryRow(ctx, `SELECT fcm_credentials_enc, fcm_project_id, fcm_client_email, fcm_updated_at
		FROM schools WHERE id = $1`, schoolID).Scan(&p.CredentialsEncrypted, &p.ProjectID, &p.ClientEmail, &p.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &p, nil
}

// SetPushSettings stores (or, with nil credentials, removes) the school's Firebase key.
func SetPushSettings(ctx context.Context, q DBTX, schoolID string, encrypted []byte, projectID, clientEmail string) error {
	tag, err := q.Exec(ctx, `UPDATE schools SET fcm_credentials_enc = $2, fcm_project_id = $3, fcm_client_email = $4,
		fcm_updated_at = CASE WHEN $2::bytea IS NULL THEN NULL ELSE now() END WHERE id = $1`,
		schoolID, encrypted, projectID, clientEmail)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
