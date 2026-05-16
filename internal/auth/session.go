package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// signer carries the HMAC-SHA256 key used for both the session cookie and
// the transient OAuth-flow cookie. Keep the same key for both — rotating
// it invalidates all in-flight logins and active sessions, which is the
// expected behaviour.
type signer struct {
	key []byte
}

func newSigner(key []byte) signer { return signer{key: key} }

// sessionData is what we marshal into the session cookie. Keep additions
// minimal: every byte rides in the user's cookie jar on every request.
type sessionData struct {
	Sub        string   `json:"sub"`
	Email      string   `json:"email,omitempty"`
	Name       string   `json:"name,omitempty"`
	GivenName  string   `json:"given_name,omitempty"`
	FamilyName string   `json:"family_name,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	Exp        int64    `json:"exp"`
}

// flowData carries the in-flight OAuth state between /login and /callback.
// Short-lived (~10 min) — long enough for the IdP redirect, not so long
// that an attacker can replay a stolen state cookie.
type flowData struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r,omitempty"`
	Exp      int64  `json:"exp"`
}

// sign returns base64url(payload) + "." + base64url(hmac(payload)). The
// caller picks the payload shape; sign doesn't care.
func (s signer) sign(payload []byte) string {
	b64 := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(b64))
	tag := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return b64 + "." + tag
}

// verify returns the original payload bytes if the MAC checks out.
func (s signer) verify(token string) ([]byte, error) {
	dot := strings.LastIndexByte(token, '.')
	if dot <= 0 || dot == len(token)-1 {
		return nil, errors.New("malformed signed token")
	}
	b64, tag := token[:dot], token[dot+1:]
	want, err := base64.RawURLEncoding.DecodeString(tag)
	if err != nil {
		return nil, errors.New("malformed signature")
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(b64))
	if !hmac.Equal(want, mac.Sum(nil)) {
		return nil, errors.New("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return nil, errors.New("malformed payload")
	}
	return payload, nil
}

func (s signer) signSession(d sessionData) (string, error) {
	buf, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	return s.sign(buf), nil
}

func (s signer) readSession(token string) (sessionData, error) {
	buf, err := s.verify(token)
	if err != nil {
		return sessionData{}, err
	}
	var d sessionData
	if err := json.Unmarshal(buf, &d); err != nil {
		return sessionData{}, err
	}
	if d.Exp != 0 && time.Now().Unix() > d.Exp {
		return sessionData{}, errors.New("session expired")
	}
	return d, nil
}

func (s signer) signFlow(d flowData) (string, error) {
	buf, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	return s.sign(buf), nil
}

func (s signer) readFlow(token string) (flowData, error) {
	buf, err := s.verify(token)
	if err != nil {
		return flowData{}, err
	}
	var d flowData
	if err := json.Unmarshal(buf, &d); err != nil {
		return flowData{}, err
	}
	if d.Exp != 0 && time.Now().Unix() > d.Exp {
		return flowData{}, errors.New("flow state expired")
	}
	return d, nil
}
