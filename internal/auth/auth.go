// Package auth handles the two identities a request can carry: an anonymous
// device id and, after Google sign-in, a signed session cookie naming a
// student.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// SessionCookie is the cookie name carrying the signed student session.
const SessionCookie = "tutor_session"

// SessionTTL is how long a sign-in lasts without re-authenticating.
const SessionTTL = 30 * 24 * time.Hour

var deviceIDRE = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// ValidDeviceID accepts the client-generated uuid-like ids and rejects
// anything that could not have come from our frontend.
func ValidDeviceID(id string) bool {
	return deviceIDRE.MatchString(id)
}

// Sessions signs and verifies student session cookies with an HMAC key.
type Sessions struct {
	key    []byte
	secure bool
	now    func() time.Time
}

// NewSessions returns a signer; secure controls the cookie's Secure flag and
// should be true everywhere except plain-HTTP local development.
func NewSessions(key []byte, secure bool) (*Sessions, error) {
	if len(key) < 32 {
		return nil, errors.New("session key must be at least 32 bytes")
	}
	return &Sessions{key: key, secure: secure, now: time.Now}, nil
}

// Issue writes a session cookie for studentID that expires after SessionTTL.
func (s *Sessions) Issue(w http.ResponseWriter, studentID string) {
	exp := s.now().Add(SessionTTL)
	payload := studentID + "|" + strconv.FormatInt(exp.Unix(), 10)
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + s.sign(payload)
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: value, Path: "/", Expires: exp,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
}

// Clear expires the session cookie.
func (s *Sessions) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
}

// StudentID returns the student named by a valid, unexpired session cookie on
// r, or "" when there is none. A tampered cookie is treated as absent.
func (s *Sessions) StudentID(r *http.Request) string {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	encoded, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	payload := string(raw)
	if !hmac.Equal([]byte(s.sign(payload)), []byte(sig)) {
		return ""
	}
	id, expStr, ok := strings.Cut(payload, "|")
	if !ok {
		return ""
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || s.now().Unix() > exp {
		return ""
	}
	return id
}

// sign returns the URL-safe HMAC of payload.
func (s *Sessions) sign(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// GoogleIdentity is what a verified Google ID token tells us about a person.
type GoogleIdentity struct {
	Sub   string
	Email string
	Name  string
}

// GoogleVerifier checks ID tokens minted by Google Identity Services for our
// OAuth client id.
type GoogleVerifier interface {
	Verify(ctx context.Context, idToken string) (GoogleIdentity, error)
}

// oidcVerifier is the production GoogleVerifier backed by Google's discovery
// document and JWKS.
type oidcVerifier struct {
	v *oidc.IDTokenVerifier
}

// NewGoogleVerifier fetches Google's OIDC configuration and returns a verifier
// bound to clientID.
func NewGoogleVerifier(ctx context.Context, clientID string) (GoogleVerifier, error) {
	if clientID == "" {
		return nil, errors.New("google client id is required")
	}
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("google oidc discovery: %w", err)
	}
	return &oidcVerifier{v: provider.Verifier(&oidc.Config{ClientID: clientID})}, nil
}

// Verify validates signature, issuer, audience and expiry, then extracts the
// profile claims.
func (o *oidcVerifier) Verify(ctx context.Context, idToken string) (GoogleIdentity, error) {
	tok, err := o.v.Verify(ctx, idToken)
	if err != nil {
		return GoogleIdentity{}, err
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := tok.Claims(&claims); err != nil {
		return GoogleIdentity{}, err
	}
	return GoogleIdentity{Sub: tok.Subject, Email: claims.Email, Name: claims.Name}, nil
}
