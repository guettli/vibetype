package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/guettli/vibetype/internal/lessons"
	"github.com/guettli/vibetype/internal/store"
)

// waUser adapts a store user to webauthn.User.
type waUser struct {
	id    string
	name  string
	creds []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return []byte(u.id) }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.name }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func credentialID(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

func toPasskey(c *webauthn.Credential, userID string) (store.Passkey, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return store.Passkey{}, err
	}
	return store.Passkey{
		ID: credentialID(c.ID), UserID: userID, PublicKey: c.PublicKey,
		SignCount: c.Authenticator.SignCount, Credential: raw,
	}, nil
}

// ---- ceremony state (in memory; a ceremony lives only a few minutes) ----

// putCeremony stores the state of a started passkey ceremony. It returns false
// (and answers 503) if too many ceremonies are pending, which bounds the memory
// an unauthenticated client can make us hold.
func (s *Server) putCeremony(w http.ResponseWriter, c ceremony) bool {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	key := hex.EncodeToString(b[:])
	c.expires = time.Now().Add(ceremonyTTL)
	s.mu.Lock()
	full := len(s.ceremonies) >= maxPendingCeremonies
	if !full {
		s.ceremonies[key] = c
	}
	s.mu.Unlock()
	if full {
		s.cfg.Logger.Warn("too many pending passkey ceremonies", "limit", maxPendingCeremonies)
		httpError(w, http.StatusServiceUnavailable, "too many sign-ins in progress, please retry in a few minutes")
		return false
	}
	s.setCookie(w, ceremonyCookie, key, ceremonyTTL)
	return true
}

// sweepCeremonies removes expired ceremonies; called periodically.
func (s *Server) sweepCeremonies() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.ceremonies {
		if now.After(v.expires) {
			delete(s.ceremonies, k)
		}
	}
}

func (s *Server) takeCeremony(w http.ResponseWriter, r *http.Request) (ceremony, bool) {
	ck, err := r.Cookie(ceremonyCookie)
	if err != nil {
		httpError(w, http.StatusBadRequest, "no passkey ceremony in progress")
		return ceremony{}, false
	}
	s.mu.Lock()
	c, ok := s.ceremonies[ck.Value]
	delete(s.ceremonies, ck.Value)
	s.mu.Unlock()
	s.setCookie(w, ceremonyCookie, "", -time.Second)
	if !ok || time.Now().After(c.expires) {
		httpError(w, http.StatusBadRequest, "passkey ceremony expired, please try again")
		return ceremony{}, false
	}
	return c, true
}

// ---- registration ----

func (s *Server) registerBegin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DisplayName string `json:"display_name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name, msg := cleanName(in.DisplayName)
	if msg != "" {
		httpError(w, http.StatusBadRequest, msg)
		return
	}
	u := &waUser{id: store.NewID(), name: name}
	opts, data, err := s.wa.BeginRegistration(u, registrationOptions()...)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !s.putCeremony(w, ceremony{data: *data, user: u}) {
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) registerFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := s.takeCeremony(w, r)
	if !ok {
		return
	}
	if c.user == nil || c.addFor != "" {
		httpError(w, http.StatusBadRequest, "not a registration ceremony")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebAuthnBodyBytes)
	cred, err := s.wa.FinishRegistration(c.user, c.data, r)
	if err != nil {
		s.cfg.Logger.Info("passkey registration failed", "err", describe(err))
		httpError(w, http.StatusBadRequest, "passkey registration failed")
		return
	}
	pk, err := toPasskey(cred, c.user.id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.cfg.Store.CreateUserWithPasskey(r.Context(), store.User{ID: c.user.id, DisplayName: c.user.name}, pk); err != nil {
		s.internal(w, r, err)
		return
	}
	s.startSession(w, r, c.user.id)
}

// ---- login (discoverable credentials / passkeys) ----

func (s *Server) loginBegin(w http.ResponseWriter, r *http.Request) {
	opts, data, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !s.putCeremony(w, ceremony{data: *data}) {
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) loginFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := s.takeCeremony(w, r)
	if !ok {
		return
	}
	if c.user != nil {
		httpError(w, http.StatusBadRequest, "not a login ceremony")
		return
	}
	var lookupErr error
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.loadWAUser(r, string(userHandle))
		lookupErr = err
		return u, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebAuthnBodyBytes)
	user, cred, err := s.wa.FinishPasskeyLogin(handler, c.data, r)
	if err != nil {
		if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
			s.internal(w, r, lookupErr)
			return
		}
		s.cfg.Logger.Info("passkey login failed", "err", describe(err))
		httpError(w, http.StatusUnauthorized, "passkey login failed")
		return
	}
	wu := user.(*waUser)
	if cred.Authenticator.CloneWarning {
		// The signature counter went backwards: the credential may have been cloned.
		s.cfg.Logger.Error("passkey clone warning, login refused", "user", wu.id, "credential", credentialID(cred.ID))
		httpError(w, http.StatusUnauthorized, "passkey login refused: signature counter mismatch (possible cloned authenticator)")
		return
	}
	pk, err := toPasskey(cred, wu.id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.cfg.Store.UpdatePasskey(r.Context(), pk); err != nil {
		s.internal(w, r, fmt.Errorf("update passkey: %w", err))
		return
	}
	s.startSession(w, r, wu.id)
}

func (s *Server) loadWAUser(r *http.Request, id string) (*waUser, error) {
	u, err := s.cfg.Store.GetUser(r.Context(), id)
	if err != nil {
		return nil, err
	}
	pks, err := s.cfg.Store.PasskeysForUser(r.Context(), id)
	if err != nil {
		return nil, err
	}
	wu := &waUser{id: u.ID, name: u.DisplayName}
	for _, p := range pks {
		var c webauthn.Credential
		dec := json.NewDecoder(bytes.NewReader(p.Credential))
		if err := dec.Decode(&c); err != nil {
			return nil, fmt.Errorf("decode passkey %s: %w", p.ID, err)
		}
		wu.creds = append(wu.creds, c)
	}
	return wu, nil
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID string) {
	token, err := s.cfg.Store.CreateAuthSession(r.Context(), userID, sessionTTL)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.setCookie(w, sessionCookie, token, sessionTTL)
	u, err := s.cfg.Store.GetUser(r.Context(), userID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.cfg.Store.DeleteAuthSession(r.Context(), c.Value); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	s.setCookie(w, sessionCookie, "", -time.Second)
	w.WriteHeader(http.StatusNoContent)
}

// describe extracts the detailed message of a protocol error.
func describe(err error) string {
	var pe *protocol.Error
	if errors.As(err, &pe) && pe.DevInfo != "" {
		return pe.Details + ": " + pe.DevInfo
	}
	return err.Error()
}

// cleanName normalizes a display name; msg is non-empty if it is not acceptable.
func cleanName(raw string) (name, msg string) {
	name = strings.Join(strings.Fields(raw), " ")
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return "", fmt.Sprintf("display name must have 1-%d characters", maxNameLen)
	}
	if strings.EqualFold(name, lessons.SystemUserName) {
		// the built-in lessons are shown as "von vibetype"; nobody may look official
		return "", fmt.Sprintf("the name %q is reserved", name)
	}
	return name, ""
}

func registrationOptions(extra ...webauthn.RegistrationOption) []webauthn.RegistrationOption {
	return append([]webauthn.RegistrationOption{
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		}),
	}, extra...)
}

// ---- account: rename, list/add/delete passkeys ----

func (s *Server) renameMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DisplayName string `json:"display_name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	name, msg := cleanName(in.DisplayName)
	if msg != "" {
		httpError(w, http.StatusBadRequest, msg)
		return
	}
	u := userFrom(r.Context())
	if err := s.cfg.Store.RenameUser(r.Context(), u.ID, name); err != nil {
		s.internal(w, r, err)
		return
	}
	u.DisplayName = name
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request) {
	pks, err := s.cfg.Store.ListPasskeys(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pks)
}

const maxLabelLen = 40

// addPasskeyBegin starts registering another passkey (e.g. the phone or the
// home PC) for the logged-in user. Existing credentials are excluded so the
// same authenticator is not registered twice.
func (s *Server) addPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label string `json:"label"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	label := strings.Join(strings.Fields(in.Label), " ")
	if utf8.RuneCountInString(label) > maxLabelLen {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("label longer than %d characters", maxLabelLen))
		return
	}
	wu, err := s.loadWAUser(r, userFrom(r.Context()).ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(wu.creds))
	for _, c := range wu.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, data, err := s.wa.BeginRegistration(wu, registrationOptions(webauthn.WithExclusions(exclude))...)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !s.putCeremony(w, ceremony{data: *data, user: wu, addFor: wu.id, label: label}) {
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) addPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := s.takeCeremony(w, r)
	if !ok {
		return
	}
	u := userFrom(r.Context())
	if c.addFor == "" || c.addFor != u.ID {
		httpError(w, http.StatusBadRequest, "not an add-passkey ceremony of this user")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWebAuthnBodyBytes)
	cred, err := s.wa.FinishRegistration(c.user, c.data, r)
	if err != nil {
		s.cfg.Logger.Info("adding passkey failed", "user", u.ID, "err", describe(err))
		httpError(w, http.StatusBadRequest, "adding the passkey failed")
		return
	}
	pk, err := toPasskey(cred, u.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.cfg.Store.AddPasskey(r.Context(), pk, c.label); err != nil {
		s.internal(w, r, err)
		return
	}
	s.listPasskeys(w, r)
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request) {
	err := s.cfg.Store.DeletePasskey(r.Context(), userFrom(r.Context()).ID, r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "passkey not found")
	case errors.Is(err, store.ErrLastPasskey):
		httpError(w, http.StatusConflict, "this is your only passkey; add another one before deleting it")
	case err != nil:
		s.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
