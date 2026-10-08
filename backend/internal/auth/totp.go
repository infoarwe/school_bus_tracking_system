package auth

import (
	"bytes"
	"encoding/base64"
	"image/png"

	"github.com/pquerna/otp/totp"
)

type TOTPSetup struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
	QRCodePNG  string `json:"qr_code_png"` // data: URL, ready for an <img src>
}

func NewTOTP(accountName string) (*TOTPSetup, error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "School Bus Tracking", AccountName: accountName})
	if err != nil {
		return nil, err
	}
	img, err := key.Image(200, 200)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &TOTPSetup{
		Secret:     key.Secret(),
		OTPAuthURL: key.URL(),
		QRCodePNG:  "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

func ValidateTOTP(code, secret string) bool { return totp.Validate(code, secret) }
