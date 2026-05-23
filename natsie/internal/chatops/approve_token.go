package chatops

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// signApprovalToken returns the HMAC-SHA256 token (URL-safe base64)
// that proves the holder is authorised to apply the named manifest.
// Matches httpsrv.SignApprovalToken bit-for-bit — kept independent here
// to avoid an import cycle (httpsrv already depends on chatops). The
// token format is the public contract; keep both in sync if it changes.
func signApprovalToken(key, manifestID string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(manifestID))

	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
