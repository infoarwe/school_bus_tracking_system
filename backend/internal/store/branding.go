package store

import (
	"context"
	"time"
)

// Branding is a school's look in its white-label apps. Empty strings mean "app default".
type Branding struct {
	SchoolID       string
	SchoolName     string
	AppName        string
	PrimaryColor   string
	SecondaryColor string
	LogoKey        *string
	UpdatedAt      *time.Time
}

const brandingSelect = `SELECT id, name, brand_app_name, brand_primary_color, brand_secondary_color,
	brand_logo_key, brand_updated_at FROM schools WHERE id = $1`

func GetBranding(ctx context.Context, q DBTX, schoolID string) (*Branding, error) {
	return getBranding(ctx, q, brandingSelect, schoolID)
}

// GetBrandingForUpdate is GetBranding with the school row locked until the transaction ends.
func GetBrandingForUpdate(ctx context.Context, q DBTX, schoolID string) (*Branding, error) {
	return getBranding(ctx, q, brandingSelect+` FOR UPDATE`, schoolID)
}

func getBranding(ctx context.Context, q DBTX, sql, schoolID string) (*Branding, error) {
	var b Branding
	err := q.QueryRow(ctx, sql, schoolID).
		Scan(&b.SchoolID, &b.SchoolName, &b.AppName, &b.PrimaryColor, &b.SecondaryColor, &b.LogoKey, &b.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &b, nil
}

// SetBrandingText updates the app name and colours.
func SetBrandingText(ctx context.Context, q DBTX, schoolID, appName, primary, secondary string) error {
	tag, err := q.Exec(ctx, `UPDATE schools SET brand_app_name = $2, brand_primary_color = $3,
		brand_secondary_color = $4, brand_updated_at = now() WHERE id = $1`, schoolID, appName, primary, secondary)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetBrandingLogo points the school at a stored logo (nil removes it).
func SetBrandingLogo(ctx context.Context, q DBTX, schoolID string, key *string) error {
	tag, err := q.Exec(ctx, `UPDATE schools SET brand_logo_key = $2, brand_updated_at = now() WHERE id = $1`,
		schoolID, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LogoInUse reports whether a stored logo key is the current logo of some school.
// Replaced or removed logos are not served even if their file still exists.
func LogoInUse(ctx context.Context, q DBTX, key string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schools WHERE brand_logo_key = $1)`, key).Scan(&ok)
	return ok, err
}
