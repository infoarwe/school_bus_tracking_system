package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
)

// SMSSender delivers OTP codes. Replace LogSender with the real provider once chosen (S0-03).
type SMSSender interface {
	SendOTP(ctx context.Context, mobile, code string) error
}

// LogSender writes the code to the server log instead of sending an SMS. Development only.
type LogSender struct{}

func (LogSender) SendOTP(_ context.Context, mobile, code string) error {
	slog.Info("OTP (dev: not sent by SMS)", "mobile", mobile, "code", code)
	return nil
}

func GenerateOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
