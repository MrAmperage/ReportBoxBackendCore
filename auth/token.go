package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// jsonObject rejects duplicate names at every depth, including escaped aliases,
// invalid UTF-8, trailing values and excessive nesting before JWT parsing.
func jsonObject(data []byte) (map[string]json.RawMessage, bool) {
	if !utf8.Valid(data) {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if !jsonValue(d, 0) {
		return nil, false
	}
	if _, e := d.Token(); e != io.EOF {
		return nil, false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}
func jsonValue(d *json.Decoder, depth int) bool {
	if depth > 24 {
		return false
	}
	t, e := d.Token()
	if e != nil {
		return false
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]struct{}{}
		for d.More() {
			k, e := d.Token()
			s, ok := k.(string)
			if e != nil || !ok {
				return false
			}
			if _, ok = seen[s]; ok {
				return false
			}
			seen[s] = struct{}{}
			if !jsonValue(d, depth+1) {
				return false
			}
		}
		end, e := d.Token()
		return e == nil && end == json.Delim('}')
	case json.Delim('['):
		for d.More() {
			if !jsonValue(d, depth+1) {
				return false
			}
		}
		end, e := d.Token()
		return e == nil && end == json.Delim(']')
	case json.Delim('}'), json.Delim(']'):
		return false
	default:
		return true
	}
}
func textClaim(m map[string]json.RawMessage, k string, max int, required bool) (string, bool) {
	raw, ok := m[k]
	if !ok {
		return "", !required
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || !validText(s, max) {
		return "", false
	}
	return s, true
}
func dateClaim(m map[string]json.RawMessage, k string, required bool) (time.Time, bool) {
	raw, ok := m[k]
	if !ok {
		return time.Time{}, !required
	}
	// Accept integer Unix seconds only; avoids fractional/overflow ambiguity.
	n, e := strconv.ParseInt(string(raw), 10, 64)
	if e != nil || n < 0 || n > 253402300799 {
		return time.Time{}, false
	}
	return time.Unix(n, 0).UTC(), true
}
func audiences(m map[string]json.RawMessage, want string) ([]string, bool) {
	raw, ok := m["aud"]
	if !ok {
		return nil, false
	}
	var a []string
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, false
		}
		a = []string{s}
	} else if json.Unmarshal(raw, &a) != nil {
		return nil, false
	}
	if len(a) == 0 || len(a) > 32 {
		return nil, false
	}
	found := false
	seen := map[string]bool{}
	for _, s := range a {
		if !validText(s, 512) || seen[s] {
			return nil, false
		}
		seen[s] = true
		found = found || s == want
	}
	return a, found
}
func keyIDValid(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return s != "." && s != ".."
}
func (v *Validator) preflight(raw string, now time.Time) (Identity, error) {
	bad := Identity{}
	if len(raw) == 0 || len(raw) > v.cfg.MaxTokenBytes || strings.Count(raw, ".") != 2 {
		return bad, ErrInvalidToken
	}
	parts := strings.Split(raw, ".")
	decoded := make([][]byte, 3)
	for n, p := range parts {
		if p == "" || strings.ContainsAny(p, "\r\n \t=") {
			return bad, ErrInvalidToken
		}
		b, e := base64.RawURLEncoding.Strict().DecodeString(p)
		if e != nil || len(b) == 0 {
			return bad, ErrInvalidToken
		}
		decoded[n] = b
	}
	h, ok := jsonObject(decoded[0])
	if !ok {
		return bad, ErrInvalidToken
	}
	// Only these JOSE fields are supported; never accept embedded keys, remote
	// key URLs, crit, unencoded payloads or other extensions.
	if len(h) != 3 {
		return bad, ErrInvalidToken
	}
	alg, ok := textClaim(h, "alg", 16, true)
	if !ok || alg != v.cfg.Algorithm {
		return bad, ErrInvalidToken
	}
	typ, ok := textClaim(h, "typ", 64, true)
	if !ok || typ != v.cfg.TokenType {
		return bad, ErrInvalidToken
	}
	kid, ok := textClaim(h, "kid", 128, true)
	if !ok || !keyIDValid(kid) {
		return bad, ErrInvalidToken
	}
	m, ok := jsonObject(decoded[1])
	if !ok {
		return bad, ErrInvalidToken
	}
	i := Identity{owner: v, algorithm: alg, keyID: kid, tokenType: typ, claims: m}
	i.issuer, ok = textClaim(m, "iss", 512, true)
	if !ok || i.issuer != v.cfg.Issuer {
		return bad, ErrInvalidToken
	}
	i.subject, ok = textClaim(m, "sub", 512, true)
	if !ok {
		return bad, ErrInvalidToken
	}
	i.audience, ok = audiences(m, v.cfg.Audience)
	if !ok {
		return bad, ErrInvalidToken
	}
	i.tokenID, ok = textClaim(m, "jti", 512, false)
	if !ok {
		return bad, ErrInvalidToken
	}
	i.expiresAt, ok = dateClaim(m, "exp", true)
	if !ok {
		return bad, ErrInvalidToken
	}
	i.issuedAt, ok = dateClaim(m, "iat", false)
	if !ok {
		return bad, ErrInvalidToken
	}
	i.notBefore, ok = dateClaim(m, "nbf", false)
	if !ok {
		return bad, ErrInvalidToken
	}
	if !i.issuedAt.IsZero() && !i.expiresAt.After(i.issuedAt) || !i.notBefore.IsZero() && !i.expiresAt.After(i.notBefore) || !v.timeValid(i, now) {
		return bad, ErrInvalidToken
	}
	return i, nil
}
func (v *Validator) timeValid(i Identity, now time.Time) bool {
	return now.Before(i.expiresAt.Add(v.cfg.ClockSkew)) && (i.notBefore.IsZero() || !now.Add(v.cfg.ClockSkew).Before(i.notBefore)) && (i.issuedAt.IsZero() || !now.Add(v.cfg.ClockSkew).Before(i.issuedAt))
}
