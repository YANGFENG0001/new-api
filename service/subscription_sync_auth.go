package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var errSubscriptionSyncUnauthorized = errors.New("Synchronization authentication failed")

func validateSubscriptionSyncRefresh(input model.SubscriptionSyncSourceInput) error {
	s := input.SubscriptionSyncSource
	mode := s.RefreshMode
	if mode == "" {
		mode = "manual"
	}
	if !slices.Contains([]string{"manual", "login", "oauth"}, mode) || s.RefreshBefore < 0 || s.RefreshBefore > 604800 || len(s.RefreshURL) > 4096 {
		return errors.New("Invalid authentication renewal settings")
	}
	if mode == "manual" {
		if s.AutoRefresh || s.RefreshURL != "" {
			return errors.New("Manual tokens cannot be automatically reissued")
		}
		if input.Request != nil && (input.Request.AuthType == "bearer" || input.Request.AuthType == "header") && input.Request.Token == "" {
			return errors.New("Invalid synchronization authentication or request")
		}
		return nil
	}
	// Require HTTPS, or an explicitly authorized literal loopback tunnel. Private
	// networks are not inherently encrypted and are not a TLS exception.
	u, err := url.Parse(s.URL)
	a, e := url.Parse(s.RefreshURL)
	if err != nil || e != nil || u.Hostname() == "" || a.Hostname() == "" || a.User != nil || a.RawQuery != "" || a.Fragment != "" || a.Scheme != u.Scheme || !strings.EqualFold(a.Host, u.Host) || strings.ContainsAny(a.Host, "%\\") || strings.ContainsAny(s.RefreshURL, "\r\n") {
		return errors.New("Authentication endpoint must use the same origin as the usage source")
	}
	ip, ipErr := netip.ParseAddr(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && s.AllowHTTP && ipErr == nil && ip.IsLoopback() && allowedSyncAddress(ip, s.PrivateIPs)) {
		return errors.New("Automatic authentication requires HTTPS or an authorized loopback tunnel")
	}
	endpoint := s
	endpoint.URL = s.RefreshURL
	endpoint.RefreshMode = "manual"
	endpoint.RefreshURL = ""
	endpoint.AutoRefresh = false
	if err := ValidateSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: endpoint, Mapping: input.Mapping}); err != nil {
		return err
	}
	// Authentication retries are only safe for read-only GET queries.
	if s.Method != "GET" {
		return errors.New("Authentication renewal requires a read-only GET usage source")
	}
	if input.Request != nil {
		r := input.Request
		if r.AuthType != "bearer" || len(r.RefreshToken) > 16384 || len(r.ClientID) > 1024 || len(r.ClientSecret) > 16384 || strings.ContainsAny(r.RefreshToken+r.ClientID+r.ClientSecret, "\r\n\x00") || (mode == "login" && (r.Username == "" || r.Password == "")) || (mode == "oauth" && r.RefreshToken == "") {
			return errors.New("Invalid authentication renewal settings")
		}
		// A second credential in headers/query/body would defeat candidate validation.
		for k := range r.Headers {
			if strings.EqualFold(k, "Authorization") {
				return errors.New("Use the dedicated bearer token field for authentication renewal")
			}
		}
		if len(r.Query) > 0 || r.Body != "" {
			return errors.New("Authentication renewal requires an unambiguous bearer request")
		}
	}
	return nil
}

func encryptSubscriptionSyncRequest(s model.SubscriptionSyncSource, r model.SubscriptionSyncRequest) (string, error) {
	aead, err := subscriptionSyncCipher()
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(r)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, raw, []byte("subscription-sync-v1:"+s.URL))), nil
}

func decryptSubscriptionSyncRequest(s model.SubscriptionSyncSource) (model.SubscriptionSyncRequest, error) {
	r := model.SubscriptionSyncRequest{}
	if s.SecretPayload == "" {
		return r, nil
	}
	aead, err := subscriptionSyncCipher()
	if err != nil {
		return r, err
	}
	raw, err := base64.RawStdEncoding.DecodeString(s.SecretPayload)
	if err != nil || len(raw) < aead.NonceSize() {
		return r, errors.New("Synchronization credentials could not be decrypted")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("subscription-sync-v1:"+s.URL))
	if err != nil || common.Unmarshal(plain, &r) != nil {
		return r, errors.New("Synchronization credentials could not be decrypted")
	}
	return r, nil
}

// action is check, refresh or replace. No secrets are returned. A check may
// extend a sliding session upstream, but never pretends to know its expiry.
func ManageSubscriptionSyncAuthentication(ctx context.Context, id, actorID int, action, replacement string, version, revision int64) (*model.SubscriptionSyncSource, error) {
	source, _, err := manageSubscriptionSyncAuthentication(ctx, id, actorID, action, replacement, version, revision)
	return source, err
}

func manageSubscriptionSyncAuthentication(ctx context.Context, id, actorID int, action, replacement string, version, revision int64) (*model.SubscriptionSyncSource, []byte, error) {
	if !slices.Contains([]string{"check", "refresh", "replace"}, action) {
		return nil, nil, errors.New("Invalid authentication action")
	}
	var s model.SubscriptionSyncSource
	if err := model.DB.First(&s, id).Error; err != nil {
		return nil, nil, err
	}
	if s.Version != version || s.CredentialRevision != revision {
		return nil, nil, errors.New("Synchronization configuration changed; reload before saving")
	}
	r, err := decryptSubscriptionSyncRequest(s)
	if err != nil {
		return nil, nil, err
	}
	if action == "refresh" {
		if s.RefreshMode != "login" && s.RefreshMode != "oauth" {
			return nil, nil, errors.New("Configure login or a documented refresh endpoint before reissuing tokens")
		}
		var mapping model.SubscriptionSyncMapping
		if err = common.UnmarshalJsonStr(s.MappingJSON, &mapping); err != nil {
			return nil, nil, errors.New("Invalid synchronization field mapping")
		}
		if err = validateSubscriptionSyncRefresh(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: s, Request: &r, Mapping: mapping}); err != nil {
			return nil, nil, err
		}
	}
	if action == "replace" {
		if (r.AuthType != "bearer" && r.AuthType != "header") || replacement == "" || len(replacement) > 16384 || strings.ContainsAny(replacement, "\r\n\x00") {
			return nil, nil, errors.New("Invalid synchronization authentication or request")
		}
		r.Token = replacement
	}
	if s.Method != "GET" {
		return nil, nil, errors.New("Authentication renewal requires a read-only GET usage source")
	}
	lease := common.GetUUID()
	if err = model.ClaimSubscriptionSyncAuthentication(s, lease, action); err != nil {
		return nil, nil, err
	}
	expires := int64(0)
	var encrypted *string
	var verified []byte
	retry := int64(600)
	if action == "refresh" {
		r, expires, err = issueSubscriptionSyncToken(ctx, s, r)
	}
	if err == nil {
		candidate := s
		value, e := encryptSubscriptionSyncRequest(s, r)
		err = e
		if err == nil {
			candidate.SecretPayload = value
			var raw []byte
			raw, err = fetchSubscriptionSync(ctx, candidate)
			verified = raw
			// Validate the documented success envelope, not merely an HTTP 200.
			if err == nil {
				var m model.SubscriptionSyncMapping
				err = common.UnmarshalJsonStr(s.MappingJSON, &m)
				if err == nil {
					_, err = ParseSubscriptionSyncResponse(raw, m, model.GetDBTimestamp())
				}
			}
			if err == nil && action != "check" {
				encrypted = &value
			}
		}
	}
	message := ""
	if err != nil {
		message = err.Error()
		var rate *subscriptionSyncRateLimitError
		if errors.As(err, &rate) {
			retry = max(retry, rate.RetryAfter)
		}
	}
	finishErr := model.FinishSubscriptionSyncAuthentication(s, lease, action, encrypted, expires, message, retry, actorID)
	if finishErr != nil {
		return nil, nil, finishErr
	}
	if err != nil {
		return nil, nil, err
	}
	if err = model.DB.First(&s, id).Error; err != nil {
		return nil, nil, err
	}
	s.HasCredentials = s.SecretPayload != ""
	if s.RefreshMode == "" {
		s.RefreshMode = "manual"
	}
	if s.RefreshBefore == 0 {
		s.RefreshBefore = 86400
	}
	return &s, verified, nil
}

func issueSubscriptionSyncToken(ctx context.Context, s model.SubscriptionSyncSource, r model.SubscriptionSyncRequest) (model.SubscriptionSyncRequest, int64, error) {
	var body string
	contentType := "application/json"
	if s.RefreshMode == "login" {
		raw, err := common.Marshal(map[string]string{"username": r.Username, "password": r.Password})
		if err != nil {
			return r, 0, err
		}
		body = string(raw)
	} else {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {r.RefreshToken}}
		if r.ClientID != "" {
			form.Set("client_id", r.ClientID)
		}
		// RFC 6749 client_secret_post; no implicit provider-specific refresh protocol.
		if r.ClientSecret != "" {
			form.Set("client_secret", r.ClientSecret)
		}
		body = form.Encode()
		contentType = "application/x-www-form-urlencoded"
	}
	request, err := http.NewRequestWithContext(ctx, "POST", s.RefreshURL, strings.NewReader(body))
	if err != nil {
		return r, 0, errors.New("Invalid authentication renewal settings")
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	issuedAt := model.GetDBTimestamp()
	raw, err := doSubscriptionSyncRequest(ctx, s, request)
	if err != nil {
		return r, 0, err
	}
	var response map[string]any
	if common.Unmarshal(raw, &response) != nil {
		return r, 0, errors.New("Invalid authentication token response")
	}
	tokenField := "access_token"
	if s.RefreshMode == "login" {
		tokenField = "token"
		if response["ok"] != true {
			return r, 0, errors.New("Authentication renewal failed")
		}
	}
	token, ok := response[tokenField].(string)
	if !ok || token == "" || len(token) > 16384 || strings.ContainsAny(token, "\r\n\x00") {
		return r, 0, errors.New("Invalid authentication token response")
	}
	if s.RefreshMode == "oauth" {
		kind, ok := response["token_type"].(string)
		if !ok || !strings.EqualFold(kind, "Bearer") {
			return r, 0, errors.New("Invalid authentication token response")
		}
		if value, exists := response["refresh_token"]; exists {
			rotated, valid := value.(string)
			if !valid || rotated == "" || len(rotated) > 16384 || strings.ContainsAny(rotated, "\r\n\x00") {
				return r, 0, errors.New("Invalid authentication token response")
			}
			r.RefreshToken = rotated
		}
	}
	expires := int64(0)
	if value, exists := response["expires_in"]; exists {
		ttl, ok := value.(float64)
		if !ok || math.IsNaN(ttl) || math.IsInf(ttl, 0) || math.Trunc(ttl) != ttl || ttl <= 0 || ttl > 315360000 {
			return r, 0, errors.New("Invalid authentication token response")
		}
		expires = issuedAt + int64(ttl)
	}
	r.AuthType = "bearer"
	r.Token = token
	return r, expires, nil
}

func fetchSubscriptionSyncWithAuthentication(ctx context.Context, s model.SubscriptionSyncSource, actorID int, preview bool) ([]byte, error) {
	if preview {
		return fetchSubscriptionSync(ctx, s)
	}
	// Reload to avoid retrying a credential already rotated by another instance.
	var current model.SubscriptionSyncSource
	if err := model.DB.First(&current, s.Id).Error; err != nil {
		return nil, errors.New("Synchronization request failed; check destination, TLS, authentication and timeout")
	}
	if current.Version != s.Version {
		return nil, errors.New("Synchronization configuration changed; retry the request")
	}
	s = current
	now := model.GetDBTimestamp()
	if s.AutoRefresh && s.AuthNextAttempt <= now && !s.RefreshBlocked && s.TokenExpiresAt > 0 && s.TokenExpiresAt <= now+s.RefreshBefore {
		_, raw, err := manageSubscriptionSyncAuthentication(ctx, s.Id, actorID, "refresh", "", s.Version, s.CredentialRevision)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
	raw, err := fetchSubscriptionSync(ctx, s)
	if !errors.Is(err, errSubscriptionSyncUnauthorized) || !s.AutoRefresh {
		if err == nil {
			model.DB.Model(&model.SubscriptionSyncSource{}).Where("id = ? AND version = ? AND (credential_revision = ? OR (credential_revision IS NULL AND ? = 0))", s.Id, s.Version, s.CredentialRevision, s.CredentialRevision).Update("last_auth_success", model.GetDBTimestamp())
		}
		return raw, err
	}
	_, verified, e := manageSubscriptionSyncAuthentication(ctx, s.Id, actorID, "refresh", "", s.Version, s.CredentialRevision)
	if e != nil {
		return nil, e
	}
	// Candidate validation is the single retry; reuse its body without a third read.
	return verified, nil
}
