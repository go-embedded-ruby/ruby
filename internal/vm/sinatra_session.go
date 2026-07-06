// Copyright (c) the go-embedded-ruby/ruby authors
//
// SPDX-License-Identifier: BSD-3-Clause

package vm

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"strings"

	"github.com/go-ruby-marshal/marshal"
	rack "github.com/go-ruby-rack/rack"

	"github.com/go-embedded-ruby/ruby/internal/object"
)

// This file implements Sinatra's `enable :sessions` cookie session store over
// the go-ruby-sinatra binding — the piece PR #157 deferred. It mirrors MRI
// Sinatra's default session middleware (Rack::Session::Cookie):
//
//   - The session is a Hash the `session` helper hands the handler; mutations
//     (session[:k]=v, session.clear) persist for the request.
//   - On dispatch it is LOADED from the request's "rack.session" cookie: the
//     cookie value is "<data>--<digest>" where data = base64(Marshal.dump(hash))
//     and digest = HMAC-SHA1(session_secret, data). A missing, malformed, or
//     tampered cookie (bad HMAC) yields a fresh empty session.
//   - After dispatch it is SAVED back into a Set-Cookie header (HttpOnly, path=/)
//     when sessions are enabled, re-serializing + re-signing the current hash.
//
// Byte-exact vs functional (see the PR / conformance doc): the serialization is
// byte-identical to MRI's default Rack::Session::Cookie coder (Base64::Marshal)
// + SHA1 HMAC given the SAME session_secret — go-ruby-marshal produces
// MRI-identical Marshal bytes and Go's base64/HMAC match Rack's. What is NOT
// byte-reproducible across an independent MRI process is the cookie *string*
// when the secret is auto-generated (each process picks its own), and any extra
// keys a full Rack::Protection stack injects (session_id/csrf) — those are out
// of scope here. The through-binary proof is therefore the functional round-trip
// (count increments across requests) plus tamper rejection, both of which an MRI
// Sinatra app reproduces observably.

// sinatraSessionCookie is the cookie name Rack::Session::Cookie uses by default
// (Sinatra's session key), so a cookie written here is the one MRI reads.
const sinatraSessionCookie = "rack.session"

// sinatraRandRead is the entropy source for the auto-generated default secret,
// indirected so a test can force its (otherwise unreachable) error path.
var sinatraRandRead = rand.Read

// sinatraSessionState is the per-dispatch session: the live Hash the handler
// mutates and the signing key it is saved with.
type sinatraSessionState struct {
	hash   *object.Hash // the mutable store the `session` helper returns
	secret []byte       // HMAC signing key (settings.session_secret or the per-VM default)
}

// sinatraSessionsEnabled reports whether the app enabled sessions
// (enable :sessions / set :sessions, true).
func sinatraSessionsEnabled(merged map[string]object.Value) bool {
	v, ok := merged["sessions"]
	return ok && v.Truthy()
}

// sinatraSessionSecret returns the HMAC signing key: settings.session_secret
// when set (set :session_secret, "…"), otherwise a per-VM random default
// generated once — matching MRI Sinatra, which warns and uses a per-process
// random secret so sessions still round-trip within the running app.
func (vm *VM) sinatraSessionSecret(merged map[string]object.Value) []byte {
	if v, ok := merged["session_secret"]; ok {
		if s := sinatraStr(v); s != "" {
			return []byte(s)
		}
	}
	if vm.sinatraDefaultSecret == nil {
		b := make([]byte, 32)
		if _, err := sinatraRandRead(b); err != nil {
			// crypto/rand.Read does not fail on supported platforms; fall back to a
			// fixed key so signing still functions if it ever does.
			b = []byte("rbgo-sinatra-default-session-secret")
		}
		vm.sinatraDefaultSecret = b
	}
	return vm.sinatraDefaultSecret
}

// loadSinatraSession builds the per-dispatch session for cls's merged settings:
// nil when sessions are disabled, otherwise the Hash decoded from the request's
// signed cookie (a fresh empty Hash when the cookie is absent/malformed/tampered).
func (vm *VM) loadSinatraSession(merged map[string]object.Value, env rack.Env) *sinatraSessionState {
	if !sinatraSessionsEnabled(merged) {
		return nil
	}
	st := &sinatraSessionState{hash: object.NewHash(), secret: vm.sinatraSessionSecret(merged)}
	if raw, ok := rack.ParseCookies(env).Get(sinatraSessionCookie); ok {
		if cookie, ok := raw.(string); ok {
			if h := decodeSinatraSession(cookie, st.secret); h != nil {
				st.hash = h
			}
		}
	}
	return st
}

// decodeSinatraSession verifies and decodes a "<data>--<digest>" session cookie,
// returning the stored Hash or nil when the cookie is malformed, the HMAC does
// not verify (tampered or a different secret), or the payload is not a Hash.
func decodeSinatraSession(cookie string, secret []byte) *object.Hash {
	idx := strings.LastIndex(cookie, "--")
	if idx < 0 {
		return nil
	}
	data, digest := cookie[:idx], cookie[idx+2:]
	if !verifySinatraSession(data, digest, secret) {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil
	}
	mv, err := marshal.Load(raw)
	if err != nil {
		return nil
	}
	h, ok := fromMarshalValue(mv, map[marshal.Value]object.Value{}).(*object.Hash)
	if !ok {
		return nil
	}
	return h
}

// saveSinatraSession serializes + signs st's current Hash and appends the
// Set-Cookie header (HttpOnly, path=/) — Rack::Session::Cookie commits the
// session on every response, so the cookie always reflects the latest state.
func (vm *VM) saveSinatraSession(st *sinatraSessionState, headers *rack.Headers) {
	data := base64.StdEncoding.EncodeToString(marshal.Dump(toMarshalValue(st.hash, map[object.Value]marshal.Value{})))
	value := data + "--" + signSinatraSession(data, st.secret)
	// The cookie key "rack.session" is a valid RFC6265 key, so the only error
	// SetCookieHeaderInto can report cannot occur here.
	_ = rack.SetCookieHeaderInto(headers, sinatraSessionCookie, rack.CookieValue{Value: value, Path: "/", HTTPOnly: true})
}

// signSinatraSession returns the hex HMAC-SHA1 of data under secret, matching
// Rack::Session::Cookie's default generate_hmac (OpenSSL::HMAC.hexdigest SHA1).
func signSinatraSession(data string, secret []byte) string {
	mac := hmac.New(sha1.New, secret)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifySinatraSession reports whether digest is the valid signature of data
// under secret, using a constant-time comparison (Rack::Session::Cookie's
// secure_compare).
func verifySinatraSession(data, digest string, secret []byte) bool {
	return hmac.Equal([]byte(signSinatraSession(data, secret)), []byte(digest))
}
