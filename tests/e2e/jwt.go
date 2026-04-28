//go:build e2e

package e2e

import (
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

func mintPairingJWT(signingKey, customerID, employeeID string) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)

	jti := make([]byte, 16)
	if _, err := cryptorand.Read(jti); err != nil {
		return "", err
	}
	now := time.Now().Unix()
	payload := map[string]any{
		"iss":         "api.prbe.ai",
		"aud":         "agent-tap",
		"customer_id": customerID,
		"sub":         employeeID,
		"jti":         fmt.Sprintf("%x", jti),
		"iat":         now,
		"exp":         now + 600,
	}
	pb, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(hb)
	payloadB64 := base64.RawURLEncoding.EncodeToString(pb)
	signing := headerB64 + "." + payloadB64

	mac := hmac.New(sha256.New, []byte(signingKey))
	_, _ = mac.Write([]byte(signing))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signing + "." + sig, nil
}
