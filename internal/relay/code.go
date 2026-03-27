package relay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	displayCodePrefix  = "relaybot_"
	modernCodeLength   = 20
	modernCodeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

type HMACCodeManager struct {
	secret []byte
}

func NewHMACCodeManager(secret string) *HMACCodeManager {
	return &HMACCodeManager{secret: []byte(secret)}
}

func (m *HMACCodeManager) Generate() (string, string, string, error) {
	body, err := randomString(modernCodeLength, modernCodeAlphabet)
	if err != nil {
		return "", "", "", err
	}

	display := displayCodePrefix + body
	return display, m.Hash(display), body[len(body)-4:], nil
}

func (m *HMACCodeManager) Normalize(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", ErrInvalidCode
	}
	if len(candidate) != len(displayCodePrefix)+modernCodeLength {
		return "", ErrInvalidCode
	}
	if !strings.EqualFold(candidate[:len(displayCodePrefix)], displayCodePrefix) {
		return "", ErrInvalidCode
	}

	body := candidate[len(displayCodePrefix):]
	switch {
	case isModernCodeBody(body):
		return displayCodePrefix + body, nil
	}
	return "", ErrInvalidCode
}

func (m *HMACCodeManager) Hash(normalized string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

func compactCode(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isModernCodeBody(body string) bool {
	for _, ch := range body {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'A' && ch <= 'Z':
		case ch >= 'a' && ch <= 'z':
		default:
			return false
		}
	}
	return true
}

func randomString(length int, alphabet string) (string, error) {
	if length <= 0 {
		return "", nil
	}

	limit := byte(256 - (256 % len(alphabet)))
	out := make([]byte, length)
	buf := make([]byte, length)
	for index := 0; index < length; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, current := range buf {
			if current >= limit {
				continue
			}
			out[index] = alphabet[int(current)%len(alphabet)]
			index++
			if index == length {
				return string(out), nil
			}
		}
	}
	return string(out), nil
}
