package relay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	codePrefix        = "RELAYBOT_"
	codeGroupSize     = 4
	codeEntropyBytes  = 10
	codeEncodedLength = 16
	crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

type HMACCodeManager struct {
	secret []byte
}

func NewHMACCodeManager(secret string) *HMACCodeManager {
	return &HMACCodeManager{secret: []byte(secret)}
}

func (m *HMACCodeManager) Generate() (string, string, string, error) {
	raw := make([]byte, codeEntropyBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", err
	}

	encoded := crockfordEncode(raw)
	normalized := codePrefix + encoded
	display := formatDisplayCode(normalized)
	return display, m.Hash(normalized), encoded[len(encoded)-4:], nil
}

func (m *HMACCodeManager) Normalize(raw string) (string, error) {
	candidate := strings.ToUpper(strings.TrimSpace(raw))
	candidate = strings.ReplaceAll(candidate, "-", "")
	candidate = strings.ReplaceAll(candidate, " ", "")
	candidate = strings.ReplaceAll(candidate, "_", "")
	if strings.HasPrefix(candidate, "RELAYBOT") && !strings.HasPrefix(candidate, codePrefix) {
		candidate = codePrefix + strings.TrimPrefix(candidate, "RELAYBOT")
	}
	if !strings.HasPrefix(candidate, codePrefix) {
		return "", ErrInvalidCode
	}

	body := strings.TrimPrefix(candidate, codePrefix)
	if len(body) != codeEncodedLength {
		return "", ErrInvalidCode
	}
	for _, ch := range body {
		if !strings.ContainsRune(crockfordAlphabet, ch) {
			return "", ErrInvalidCode
		}
	}
	return codePrefix + body, nil
}

func (m *HMACCodeManager) Hash(normalized string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

func formatDisplayCode(normalized string) string {
	body := strings.TrimPrefix(normalized, codePrefix)
	var groups []string
	for start := 0; start < len(body); start += codeGroupSize {
		groups = append(groups, body[start:start+codeGroupSize])
	}
	return "relaybot_" + strings.Join(groups, "-")
}

func crockfordEncode(input []byte) string {
	if len(input) == 0 {
		return ""
	}

	var (
		value uint64
		bits  uint
		out   strings.Builder
	)

	out.Grow(codeEncodedLength)
	for _, current := range input {
		value = (value << 8) | uint64(current)
		bits += 8
		for bits >= 5 {
			index := (value >> (bits - 5)) & 0x1F
			out.WriteByte(crockfordAlphabet[index])
			bits -= 5
		}
	}

	if bits > 0 {
		index := (value << (5 - bits)) & 0x1F
		out.WriteByte(crockfordAlphabet[index])
	}

	encoded := out.String()
	if len(encoded) > codeEncodedLength {
		return encoded[:codeEncodedLength]
	}
	if len(encoded) == codeEncodedLength {
		return encoded
	}
	return encoded + strings.Repeat("0", codeEncodedLength-len(encoded))
}
