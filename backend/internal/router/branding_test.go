package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pavithra-thiyagarajan/school-bus-tracking/backend/internal/models"
)

func pngLogo(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.NRGBA{10, 20, 30, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// putLogo sends a file as multipart field "logo".
func (e *env) putLogo(path, token, field string, data []byte) resp {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile(field, "logo.png")
	_, _ = fw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest("PUT", path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	out := resp{Status: rec.Code}
	_ = json.Unmarshal(rec.Body.Bytes(), &out.Body)
	return out
}

// getRaw fetches a non-JSON resource (a logo).
func (e *env) getRaw(path string) (int, string, []byte, map[string]string) {
	req := httptest.NewRequest("GET", path, nil)
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	h := map[string]string{}
	for _, k := range []string{"Cache-Control", "X-Content-Type-Options", "Content-Security-Policy"} {
		h[k] = rec.Header().Get(k)
	}
	return rec.Code, rec.Header().Get("Content-Type"), body, h
}

func TestBranding(t *testing.T) {
	e := newEnv(t)
	a, b := e.school("AAA"), e.school("BBB")
	e.user(&a, models.RoleSchoolAdmin, "admin@a.test", "")
	e.user(&a, models.RoleTransportManager, "tm@a.test", "")
	e.user(&b, models.RoleSchoolAdmin, "admin@b.test", "")
	e.user(&a, models.RoleParent, "", "+919000000101")
	e.user(&b, models.RoleDriver, "", "+919000000102")
	admin, tm, otherAdmin := e.login("admin@a.test"), e.login("tm@a.test"), e.login("admin@b.test")
	parentA, driverB := e.otpLogin("9000000101", "parent"), e.otpLogin("9000000102", "driver")
	url := "/api/v1/schools/" + a + "/settings/branding"

	// Defaults: nothing set, the app shows the school name.
	d := e.do("GET", "/api/v1/branding", parentA, nil)
	e.expect(d, 200, "")
	if d.data()["display_name"] != "AAA School" || d.data()["app_name"] != "" || d.data()["logo_url"] != nil ||
		d.data()["primary_color"] != nil {
		t.Errorf("default branding = %v", d.data())
	}

	// Validation.
	bad := e.do("PUT", url, admin, map[string]any{"app_name": strings.Repeat("x", 61), "primary_color": "red",
		"secondary_color": "#12345"})
	e.expect(bad, 400, "validation_failed")
	if f := bad.Body["error"].(map[string]any)["fields"].(map[string]any); len(f) != 3 {
		t.Errorf("fields = %v", f)
	}
	e.expect(e.do("PUT", url, admin, map[string]any{"app_name": "a\nb"}), 400, "validation_failed")

	// Set name and colours (lower-case colours are normalised).
	set := e.do("PUT", url, admin, map[string]any{"app_name": " AAA Bus ", "primary_color": "#1a73e8",
		"secondary_color": "#FFAA00"})
	e.expect(set, 200, "")
	if set.data()["app_name"] != "AAA Bus" || set.data()["primary_color"] != "#1A73E8" {
		t.Errorf("after update = %v", set.data())
	}

	// Permissions: Transport Manager reads only; other schools see nothing.
	e.expect(e.do("GET", url, tm, nil), 200, "")
	e.expect(e.do("PUT", url, tm, map[string]any{"app_name": "x"}), 403, "forbidden")
	e.expect(e.putLogo(url+"/logo", tm, "logo", pngLogo(t, 32, 32)), 403, "forbidden")
	e.expect(e.do("DELETE", url+"/logo", tm, nil), 403, "forbidden")
	e.expect(e.do("GET", url, otherAdmin, nil), 404, "not_found")
	e.expect(e.do("PUT", url, otherAdmin, map[string]any{"app_name": "x"}), 404, "not_found")
	e.expect(e.putLogo(url+"/logo", otherAdmin, "logo", pngLogo(t, 32, 32)), 404, "not_found")
	e.expect(e.do("GET", url, parentA, nil), 403, "forbidden")

	// Logo upload: validation.
	e.expect(e.putLogo(url+"/logo", admin, "logo", []byte(`<svg onload="alert(1)"/>`)), 400, "validation_failed")
	e.expect(e.putLogo(url+"/logo", admin, "file", pngLogo(t, 32, 32)), 400, "validation_failed")
	e.expect(e.putLogo(url+"/logo", admin, "logo", bytes.Repeat([]byte{0x89}, testMaxLogoBytes+1)), 400, "validation_failed")
	e.expect(e.do("PUT", url+"/logo", admin, map[string]any{"logo": "x"}), 400, "invalid_upload")

	up := e.putLogo(url+"/logo", admin, "logo", pngLogo(t, 64, 64))
	e.expect(up, 200, "")
	logoURL, _ := up.data()["logo_url"].(string)
	if !strings.HasPrefix(logoURL, "/api/v1/branding/logos/") || !strings.HasSuffix(logoURL, ".png") {
		t.Fatalf("logo_url = %q", logoURL)
	}

	// The app gets its own school's branding, served without a token.
	app := e.do("GET", "/api/v1/branding", parentA, nil).data()
	if app["display_name"] != "AAA Bus" || app["logo_url"] != logoURL || app["secondary_color"] != "#FFAA00" {
		t.Errorf("app branding = %v", app)
	}
	if other := e.do("GET", "/api/v1/branding", driverB, nil).data(); other["school_id"] != b || other["logo_url"] != nil {
		t.Errorf("school B driver must get school B's branding: %v", other)
	}
	status, ctype, body, h := e.getRaw(logoURL)
	if status != 200 || ctype != "image/png" || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("logo GET = %d %s", status, ctype)
	}
	if h["X-Content-Type-Options"] != "nosniff" || !strings.Contains(h["Cache-Control"], "immutable") ||
		h["Content-Security-Policy"] != "default-src 'none'" {
		t.Errorf("logo headers = %v", h)
	}
	for _, p := range []string{"/api/v1/branding/logos/..%2f..%2fetc%2fpasswd", "/api/v1/branding/logos/x.png",
		"/api/v1/branding/logos/" + strings.Repeat("0", 32) + ".png"} {
		if s, _, _, _ := e.getRaw(p); s != 404 {
			t.Errorf("GET %s = %d, want 404", p, s)
		}
	}

	// Replacing the logo makes the old URL stop working.
	up2 := e.putLogo(url+"/logo", admin, "logo", pngLogo(t, 48, 48))
	e.expect(up2, 200, "")
	if s, _, _, _ := e.getRaw(logoURL); s != 404 {
		t.Errorf("old logo still served: %d", s)
	}
	newURL := up2.data()["logo_url"].(string)

	// Remove.
	rm := e.do("DELETE", url+"/logo", admin, nil)
	e.expect(rm, 200, "")
	if rm.data()["logo_url"] != nil {
		t.Errorf("after remove = %v", rm.data())
	}
	if s, _, _, _ := e.getRaw(newURL); s != 404 {
		t.Errorf("removed logo still served: %d", s)
	}
	e.expect(e.do("DELETE", url+"/logo", admin, nil), 200, "") // idempotent

	// Audited, with no file contents.
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action IN
		('settings.branding_update', 'settings.branding_logo_upload', 'settings.branding_logo_remove')`, a).Scan(&n)
	if n != 4 { // update, upload, upload, remove
		t.Errorf("audit entries = %d, want 4", n)
	}
}
