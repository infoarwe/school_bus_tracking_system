package store

import "context"

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
