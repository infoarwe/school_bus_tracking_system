package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/auth"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/httpx"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/storage"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/store"
	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/validate"
)

// S8-09: each school's white-label apps fetch the school's app name, colours and logo.

const (
	maxAppNameLen = 60
	// LogoURLPrefix is where logos are served; logo_url = prefix + file name.
	LogoURLPrefix = "/api/v1/branding/logos/"
	logoKeyDir    = "logos/"
)

var (
	hexColorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	// Logo file names are server-generated: 128 random bits + extension.
	logoFileRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg)$`)
)

type brandingResponse struct {
	SchoolID   string `json:"school_id"`
	SchoolName string `json:"school_name"`
	// AppName is what the school set ("" = not set); DisplayName is what the app shows.
	AppName        string     `json:"app_name"`
	DisplayName    string     `json:"display_name"`
	PrimaryColor   *string    `json:"primary_color"`
	SecondaryColor *string    `json:"secondary_color"`
	LogoURL        *string    `json:"logo_url"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

func brandingView(b *store.Branding) brandingResponse {
	res := brandingResponse{
		SchoolID: b.SchoolID, SchoolName: b.SchoolName, AppName: b.AppName, DisplayName: b.AppName,
		PrimaryColor: nonEmpty(b.PrimaryColor), SecondaryColor: nonEmpty(b.SecondaryColor), UpdatedAt: b.UpdatedAt,
	}
	if res.DisplayName == "" {
		res.DisplayName = b.SchoolName
	}
	if b.LogoKey != nil {
		u := LogoURLPrefix + strings.TrimPrefix(*b.LogoKey, logoKeyDir)
		res.LogoURL = &u
	}
	return res
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// GetBrandingSettings: any staff of the school.
func (a *API) GetBrandingSettings(w http.ResponseWriter, r *http.Request) {
	b, err := store.GetBranding(r.Context(), a.Store.Pool, chi.URLParam(r, "schoolID"))
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, brandingView(b))
}

// AppBranding is the caller's own school's branding, for the Driver and Parent
// apps (and staff). Scoped by the caller's school; there is no school ID to pass.
func (a *API) AppBranding(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil || p.SchoolID == nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	b, err := store.GetBranding(r.Context(), a.Store.Pool, *p.SchoolID)
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, brandingView(b))
}

// UpdateBrandingSettings sets the app name and colours. Super Admin or School Admin.
func (a *API) UpdateBrandingSettings(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	var req struct {
		AppName        string `json:"app_name"`
		PrimaryColor   string `json:"primary_color"`
		SecondaryColor string `json:"secondary_color"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	req.AppName = strings.TrimSpace(req.AppName)
	req.PrimaryColor = strings.ToUpper(strings.TrimSpace(req.PrimaryColor))
	req.SecondaryColor = strings.ToUpper(strings.TrimSpace(req.SecondaryColor))
	v := validate.New()
	v.Check(utf8.RuneCountInString(req.AppName) <= maxAppNameLen, "app_name",
		fmt.Sprintf("must be at most %d characters", maxAppNameLen))
	v.Check(!strings.ContainsFunc(req.AppName, unicode.IsControl), "app_name", "must be a single line of text")
	const colorMsg = "must be a colour like #1A73E8, or empty for the app default"
	v.Check(req.PrimaryColor == "" || hexColorRe.MatchString(req.PrimaryColor), "primary_color", colorMsg)
	v.Check(req.SecondaryColor == "" || hexColorRe.MatchString(req.SecondaryColor), "secondary_color", colorMsg)
	if !v.OK() {
		httpx.ValidationError(w, v.Errors())
		return
	}

	ctx := r.Context()
	var after *store.Branding
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetBrandingForUpdate(ctx, q, schoolID)
		if err != nil {
			return err
		}
		if err := store.SetBrandingText(ctx, q, schoolID, req.AppName, req.PrimaryColor, req.SecondaryColor); err != nil {
			return err
		}
		if after, err = store.GetBranding(ctx, q, schoolID); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "settings.branding_update", EntityType: "school", EntityID: &schoolID,
			Before: brandingAudit(before), After: brandingAudit(after),
		})
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	httpx.JSON(w, http.StatusOK, brandingView(after))
}

func brandingAudit(b *store.Branding) map[string]any {
	return map[string]any{"app_name": b.AppName, "primary_color": b.PrimaryColor,
		"secondary_color": b.SecondaryColor, "logo_key": b.LogoKey}
}

// UploadBrandingLogo replaces the school's logo: multipart/form-data with one
// file field "logo" (PNG, JPEG or WebP, at most MaxLogoBytes). The image is
// re-encoded before it is stored. Super Admin or School Admin.
func (a *API) UploadBrandingLogo(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	data, ok := a.readLogoUpload(w, r)
	if !ok {
		return
	}
	img, err := storage.ProcessImage(data)
	if ie, bad := storage.IsImageError(err); bad {
		httpx.ValidationError(w, validate.Errors{"logo": ie.Reason})
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}

	ctx := r.Context()
	key := logoKeyDir + storage.RandomName() + "." + img.Ext
	if err := a.Files.Put(ctx, key, img.Data); err != nil {
		internalError(w, r, err)
		return
	}
	var old *string
	var after *store.Branding
	err = a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetBrandingForUpdate(ctx, q, schoolID)
		if err != nil {
			return err
		}
		old = before.LogoKey
		if err := store.SetBrandingLogo(ctx, q, schoolID, &key); err != nil {
			return err
		}
		if after, err = store.GetBranding(ctx, q, schoolID); err != nil {
			return err
		}
		return a.audit(ctx, q, r, store.AuditEntry{
			SchoolID: &schoolID, Action: "settings.branding_logo_upload", EntityType: "school", EntityID: &schoolID,
			Before: map[string]any{"logo_key": before.LogoKey},
			After: map[string]any{"logo_key": key, "content_type": img.ContentType, "bytes": len(img.Data),
				"width": img.Width, "height": img.Height},
		})
	})
	if err != nil {
		a.deleteFile(context.WithoutCancel(ctx), key) // not referenced: do not leave it behind
		storeError(w, r, err, nil)
		return
	}
	if old != nil {
		a.deleteFile(context.WithoutCancel(ctx), *old)
	}
	httpx.JSON(w, http.StatusOK, brandingView(after))
}

// readLogoUpload reads the "logo" part of a multipart body, enforcing the size limit.
func (a *API) readLogoUpload(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	limit := a.MaxLogoBytes
	tooLarge := func() ([]byte, bool) {
		httpx.ValidationError(w, validate.Errors{"logo": fmt.Sprintf("must be at most %s", formatBytes(limit))})
		return nil, false
	}
	// Room for the multipart headers around the file.
	r.Body = http.MaxBytesReader(w, r.Body, limit+64<<10)
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_upload", "Send the file as multipart/form-data in a field named \"logo\".")
		return nil, false
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return tooLarge()
		}
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid_upload", "The upload could not be read.")
			return nil, false
		}
		if part.FormName() != "logo" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, limit+1))
		_ = part.Close()
		if errors.As(err, &mbe) || int64(len(data)) > limit {
			return tooLarge()
		}
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid_upload", "The upload could not be read.")
			return nil, false
		}
		return data, true
	}
	httpx.ValidationError(w, validate.Errors{"logo": "choose an image file"})
	return nil, false
}

func formatBytes(n int64) string {
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%d KB", n>>10)
}

// DeleteBrandingLogo removes the school's logo (the apps show their default). Super Admin or School Admin.
func (a *API) DeleteBrandingLogo(w http.ResponseWriter, r *http.Request) {
	schoolID := chi.URLParam(r, "schoolID")
	ctx := r.Context()
	var old *string
	var after *store.Branding
	err := a.Store.InTx(ctx, func(q store.DBTX) error {
		before, err := store.GetBrandingForUpdate(ctx, q, schoolID)
		if err != nil {
			return err
		}
		if old = before.LogoKey; old != nil {
			if err := store.SetBrandingLogo(ctx, q, schoolID, nil); err != nil {
				return err
			}
			if err := a.audit(ctx, q, r, store.AuditEntry{
				SchoolID: &schoolID, Action: "settings.branding_logo_remove", EntityType: "school", EntityID: &schoolID,
				Before: map[string]any{"logo_key": *old},
			}); err != nil {
				return err
			}
		}
		after, err = store.GetBranding(ctx, q, schoolID)
		return err
	})
	if err != nil {
		storeError(w, r, err, nil)
		return
	}
	if old != nil {
		a.deleteFile(context.WithoutCancel(ctx), *old)
	}
	httpx.JSON(w, http.StatusOK, brandingView(after))
}

// deleteFile removes a file that is no longer referenced. A failure only leaves
// an unreachable file behind (logos are served only while referenced), so it is logged.
func (a *API) deleteFile(ctx context.Context, key string) {
	if err := a.Files.Delete(ctx, key); err != nil && !errors.Is(err, storage.ErrNotFound) {
		slog.Warn("delete stored file", "key", key, "err", err)
	}
}

// BrandingLogo serves a school's current logo. Public (no token) so image
// loaders and <img> tags work: the file name is 128 random bits, and replaced
// or removed logos return 404.
func (a *API) BrandingLogo(w http.ResponseWriter, r *http.Request) {
	file := chi.URLParam(r, "file")
	if !logoFileRe.MatchString(file) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	key := logoKeyDir + file
	ctx := r.Context()
	inUse, err := store.LogoInUse(ctx, a.Store.Pool, key)
	if err != nil {
		internalError(w, r, err)
		return
	}
	if !inUse {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	f, err := a.Files.Open(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", storage.ContentTypeForKey(key))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'")
	// A new upload gets a new file name, so a file never changes.
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		slog.Debug("send logo", "err", err)
	}
}
