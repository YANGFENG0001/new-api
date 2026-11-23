package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var syncFieldPath = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

func ValidateSubscriptionSyncSource(input model.SubscriptionSyncSourceInput) error {
	s := input.SubscriptionSyncSource
	if err := validateSubscriptionSyncRefresh(input); err != nil {
		return err
	}
	if s.Id < 0 || strings.TrimSpace(s.Name) == "" || len(s.Name) > 128 || len(s.URL) > 4096 || (s.Method != "GET" && s.Method != "POST") || s.IntervalSeconds < 10 || s.IntervalSeconds > 86400 || s.StaleSeconds < 10 || s.StaleSeconds > 604800 || s.TimeoutSeconds < 1 || s.TimeoutSeconds > 60 {
		return errors.New("Invalid synchronization source")
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && s.AllowHTTP)) || strings.Contains(u.Host, "%") {
		return errors.New("Invalid synchronization URL")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("Invalid synchronization URL")
		}
	}
	path := strings.ToLower(path.Clean(u.Path))
	if slices.Contains([]string{"/api/panel/usage-probe", "/api/panel/probe", "/admin/vms/probe-all"}, path) {
		return errors.New("Active probe endpoints cannot be used for synchronization")
	}
	for _, ip := range strings.FieldsFunc(s.PrivateIPs, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		addr, e := netip.ParseAddr(ip)
		if e != nil || forbiddenSyncAddress(addr) {
			return errors.New("Invalid private synchronization address")
		}
	}
	if ip, e := netip.ParseAddr(u.Hostname()); e == nil && !allowedSyncAddress(ip, s.PrivateIPs) {
		return errors.New("Synchronization destination is not allowed")
	}
	m := input.Mapping
	if (m.Profile != "" && m.Profile != "vm2api") || len(m.Cycles) == 0 || len(m.Cycles) > 32 || m.AccountsPath == "" || m.IDPath == "" || m.SampledAtPath == "" || !slices.Contains([]string{"rfc3339", "unix_s", "unix_ms"}, m.TimeFormat) {
		return errors.New("Invalid synchronization field mapping")
	}
	paths := []string{m.SuccessPath, m.AccountsPath, m.IDPath, m.NamePath, m.SampledAtPath, m.SourcePath, m.ExpiryPath}
	keys := map[string]bool{}
	for _, c := range m.Cycles {
		if c.Key == "" || len(c.Key) > 64 || keys[c.Key] || !syncFieldPath.MatchString(c.Key) || strings.Contains(c.Key, ".") || len(c.Name) > 128 || c.Seconds <= 0 || c.Seconds > 315360000 || c.UtilizationPath == "" || c.ResetPath == "" || (c.Scale != "ratio" && c.Scale != "percent") {
			return errors.New("Invalid synchronization field mapping")
		}
		keys[c.Key] = true
		paths = append(paths, c.UtilizationPath, c.ResetPath, c.SampledAtPath)
	}
	for _, p := range paths {
		if len(p) > 256 || (p != "" && !syncFieldPath.MatchString(p)) {
			return errors.New("Invalid synchronization field mapping")
		}
	}
	if input.Request != nil {
		return validateSubscriptionSyncRequest(*input.Request, s.Method)
	}
	return nil
}

func validateSubscriptionSyncRequest(r model.SubscriptionSyncRequest, method string) error {
	if !slices.Contains([]string{"", "none", "bearer", "header", "basic"}, r.AuthType) || len(r.Token) > 16384 || len(r.Password) > 16384 || len(r.Username) > 1024 || len(r.Headers) > 50 || len(r.Query) > 50 || len(r.Body) > 65536 {
		return errors.New("Invalid synchronization authentication or request")
	}
	if (r.AuthType == "bearer" || r.AuthType == "header") && r.Token == "" && r.Password == "" && r.RefreshToken == "" {
		return errors.New("Invalid synchronization authentication or request")
	}
	if r.AuthType == "header" && r.AuthHeader == "" {
		return errors.New("Invalid synchronization authentication or request")
	}
	headers := http.Header{}
	for k, v := range r.Headers {
		headers.Set(k, v)
	}
	if r.AuthType == "header" {
		headers.Set(r.AuthHeader, r.Token)
	}
	for k, values := range headers {
		if !regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$").MatchString(k) || slices.Contains([]string{"Host", "Connection", "Content-Length", "Transfer-Encoding", "Upgrade", "Proxy-Authorization", "Proxy-Connection", "Accept-Encoding"}, http.CanonicalHeaderKey(k)) {
			return errors.New("Invalid synchronization request header")
		}
		for _, v := range values {
			if len(v) > 16384 || strings.ContainsAny(v, "\r\n\x00") {
				return errors.New("Invalid synchronization request header")
			}
		}
	}
	if strings.ContainsAny(r.Token+r.Username+r.Password, "\r\n\x00") {
		return errors.New("Invalid synchronization authentication or request")
	}
	for k, v := range r.Query {
		if len(k) > 256 || len(v) > 16384 || strings.ContainsAny(k+v, "\r\n\x00") {
			return errors.New("Invalid synchronization authentication or request")
		}
	}
	if r.Body != "" {
		var value any
		if method != "POST" || common.Unmarshal([]byte(r.Body), &value) != nil {
			return errors.New("Invalid synchronization JSON request body")
		}
	}
	return nil
}

func subscriptionSyncCipher() (cipher.AEAD, error) {
	secret := os.Getenv("SUBSCRIPTION_SYNC_SECRET_KEY")
	if len(secret) < 32 {
		return nil, errors.New("Configure SUBSCRIPTION_SYNC_SECRET_KEY with at least 32 characters before saving credentials")
	}
	key := sha256.Sum256([]byte("subscription-sync-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func SaveSubscriptionSyncSource(input model.SubscriptionSyncSourceInput, actorID int, privileged bool) (*model.SubscriptionSyncSource, error) {
	if input.RefreshMode == "" {
		input.RefreshMode = "manual"
	}
	if input.RefreshBefore == 0 {
		input.RefreshBefore = 86400
	}
	if err := ValidateSubscriptionSyncSource(input); err != nil {
		return nil, err
	}
	var encrypted *string
	if input.Request != nil {
		value, err := encryptSubscriptionSyncRequest(input.SubscriptionSyncSource, *input.Request)
		if err != nil {
			return nil, err
		}
		encrypted = &value
	}
	return model.SaveSubscriptionSyncSource(input, encrypted, actorID, privileged)
}

func forbiddenSyncAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.Zone() != "" || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || netip.MustParsePrefix("0.0.0.0/8").Contains(ip) || ip == netip.MustParseAddr("100.100.100.200") || ip == netip.MustParseAddr("168.63.129.16") || ip == netip.MustParseAddr("fd00:ec2::254") || netip.MustParsePrefix("64:ff9b::/96").Contains(ip) || netip.MustParsePrefix("64:ff9b:1::/48").Contains(ip) || netip.MustParsePrefix("2002::/16").Contains(ip) || netip.MustParsePrefix("2001::/32").Contains(ip)
}

func allowedSyncAddress(ip netip.Addr, privateIPs string) bool {
	ip = ip.Unmap()
	if forbiddenSyncAddress(ip) {
		return false
	}
	// Permit only globally routed addresses by default. Explicit individual IPs
	// (not a broad 'allow private networks' switch) are privileged configuration.
	private := ip.IsPrivate() || ip.IsLoopback() || !ip.IsGlobalUnicast() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("198.18.0.0/15").Contains(ip) || netip.MustParsePrefix("240.0.0.0/4").Contains(ip)
	if !private {
		return true
	}
	for _, raw := range strings.FieldsFunc(privateIPs, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		permitted, err := netip.ParseAddr(raw)
		if err == nil && permitted.Unmap() == ip {
			return true
		}
	}
	return false
}

func fetchSubscriptionSync(ctx context.Context, s model.SubscriptionSyncSource) ([]byte, error) {
	var mapping model.SubscriptionSyncMapping
	if err := common.UnmarshalJsonStr(s.MappingJSON, &mapping); err != nil {
		return nil, errors.New("Invalid synchronization field mapping")
	}
	if err := ValidateSubscriptionSyncSource(model.SubscriptionSyncSourceInput{SubscriptionSyncSource: s, Mapping: mapping}); err != nil {
		return nil, err
	}
	r, err := decryptSubscriptionSyncRequest(s)
	if err != nil {
		return nil, err
	}
	if err := validateSubscriptionSyncRequest(r, s.Method); err != nil {
		return nil, err
	}
	u, _ := url.Parse(s.URL)
	q := u.Query()
	for k, v := range r.Query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, s.Method, u.String(), strings.NewReader(r.Body))
	if err != nil {
		return nil, errors.New("Invalid synchronization request")
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	switch r.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+r.Token)
	case "header":
		req.Header.Set(r.AuthHeader, r.Token)
	case "basic":
		req.SetBasicAuth(r.Username, r.Password)
	}
	req.Header.Set("Accept", "application/json")
	if r.Body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return doSubscriptionSyncRequest(ctx, s, req)
}

// All authentication and usage calls share the same pinned DNS, TLS,
// no-proxy, no-redirect, timeout and response-size policy.
func doSubscriptionSyncRequest(ctx context.Context, s model.SubscriptionSyncSource, req *http.Request) ([]byte, error) {
	u := req.URL
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: time.Duration(s.TimeoutSeconds) * time.Second, ResponseHeaderTimeout: time.Duration(s.TimeoutSeconds) * time.Second, DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(address)
			if e != nil || !strings.EqualFold(host, u.Hostname()) {
				return nil, errors.New("Synchronization destination is not allowed")
			}
			ips, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if e != nil || len(ips) == 0 {
				return nil, errors.New("Synchronization name resolution failed")
			}
			for _, ip := range ips {
				if !allowedSyncAddress(ip, s.PrivateIPs) {
					return nil, errors.New("Synchronization destination is not allowed")
				}
			}
			dialer := net.Dialer{Timeout: time.Duration(s.TimeoutSeconds) * time.Second}
			// DNS is resolved and checked once, then the connection is pinned to that IP.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: time.Duration(s.TimeoutSeconds) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("Synchronization redirects are not allowed")
	}}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Synchronization request failed; check destination, TLS, authentication and timeout")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 {
		return nil, errSubscriptionSyncUnauthorized
	}
	if response.StatusCode == 403 {
		return nil, errors.New("Synchronization permission denied")
	}
	if response.StatusCode == 429 {
		retry := int64(0)
		header := response.Header.Get("Retry-After")
		if seconds, e := strconv.ParseInt(header, 10, 64); e == nil {
			retry = min(max(seconds, 0), int64(86400))
		} else if deadline, e := http.ParseTime(header); e == nil {
			retry = min(max(deadline.Unix()-time.Now().Unix(), 0), int64(86400))
		}
		return nil, &subscriptionSyncRateLimitError{RetryAfter: retry}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("Synchronization upstream returned an error")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
	if err != nil || len(raw) > 8*1024*1024 {
		return nil, errors.New("Synchronization response is too large or incomplete")
	}
	return raw, nil
}

// Paths deliberately support only object keys and numeric array indexes, not
// scripts, recursive descent, filters, or executable expressions.
func syncPath(root any, path string) any {
	if path == "" {
		return nil
	}
	current := root
	for _, key := range strings.Split(path, ".") {
		switch v := current.(type) {
		case map[string]any:
			current = v[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(v) {
				return nil
			}
			current = v[i]
		default:
			return nil
		}
	}
	return current
}

func syncTimestamp(v any, format string) int64 {
	var n int64
	if format == "rfc3339" {
		text, ok := v.(string)
		if !ok {
			return 0
		}
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return 0
		}
		n = parsed.Unix()
	} else {
		value, ok := v.(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 253402300799000 || math.Trunc(value) != value {
			return 0
		}
		n = int64(value)
		if format == "unix_ms" {
			n /= 1000
		}
	}
	if n <= 0 || n > 253402300799 {
		return 0
	}
	return n
}

// vm2api combines RFC3339 official resets with Unix seconds/milliseconds
// (including digit strings) from response headers. Do not relax custom sources.
func vm2apiSyncTimestamp(v any) int64 {
	if n := syncTimestamp(v, "rfc3339"); n > 0 {
		return n
	}
	var n int64
	switch value := v.(type) {
	case string:
		if len(value) != 10 && len(value) != 13 {
			return 0
		}
		for _, ch := range value {
			if ch < '0' || ch > '9' {
				return 0
			}
		}
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0
		}
		n = parsed
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 1000000000 || value > 9999999999999 {
			return 0
		}
		n = int64(value)
	default:
		return 0
	}
	if n >= 1000000000000 {
		n /= 1000
	} else if n > 9999999999 {
		return 0
	}
	if n < 1000000000 || n > 9999999999 {
		return 0
	}
	return n
}

func ParseSubscriptionSyncResponse(raw []byte, m model.SubscriptionSyncMapping, now int64) ([]model.SubscriptionSyncAccount, error) {
	var root any
	if len(raw) > 8*1024*1024 || common.Unmarshal(raw, &root) != nil {
		return nil, errors.New("Invalid synchronization JSON response")
	}
	if m.SuccessPath != "" && syncPath(root, m.SuccessPath) != true {
		return nil, errors.New("Synchronization upstream reported failure")
	}
	list, ok := syncPath(root, m.AccountsPath).([]any)
	if !ok || len(list) > 1000 {
		return nil, errors.New("Invalid synchronization account list")
	}
	timestamp := func(v any) int64 { return syncTimestamp(v, m.TimeFormat) }
	vm2api := model.IsVM2APISubscriptionSyncMapping(m)
	if vm2api {
		timestamp = vm2apiSyncTimestamp
	}
	sample := func(item any, field string) (int64, string) {
		value := syncPath(item, field)
		quality := ""
		if vm2api && (field == "last_probe" || field == "last_probe.at") {
			if probe, ok := syncPath(item, "last_probe").(map[string]any); ok {
				value = probe["at"]
				// The flattened utilization may come from headers, not last_probe.
				quality = "hint"
			} else if field == "last_probe.at" {
				value = syncPath(item, "last_probe")
			}
		}
		return timestamp(value), quality
	}
	accounts := make([]model.SubscriptionSyncAccount, 0, len(list))
	ids := map[string]bool{}
	for _, item := range list {
		id, ok := syncPath(item, m.IDPath).(string)
		if !ok || id == "" || len(id) > 128 || ids[id] {
			return nil, errors.New("Invalid or duplicate synchronization account ID")
		}
		ids[id] = true
		name, _ := syncPath(item, m.NamePath).(string)
		if len(name) > 128 {
			name = id
		}
		expiry := timestamp(syncPath(item, m.ExpiryPath))
		sampledAt, accountQuality := sample(item, m.SampledAtPath)
		if accountQuality == "hint" {
			expiry = 0
		} // No account-wide authoritative expiry sample.
		account := model.SubscriptionSyncAccount{ID: id, Name: name, Expiry: expiry, SampledAt: sampledAt, Cycles: []model.SubscriptionSyncCycle{}}
		source, _ := syncPath(item, m.SourcePath).(string)
		if len(source) > 64 {
			source = ""
		}
		for _, c := range m.Cycles {
			if syncPath(item, c.UtilizationPath) == nil {
				continue
			}
			timestampPath := m.SampledAtPath
			if c.SampledAtPath != "" {
				timestampPath = c.SampledAtPath
			}
			sampledAt, quality := sample(item, timestampPath)
			cycle := model.SubscriptionSyncCycle{SampleTimeQuality: quality, Key: c.Key, Name: model.SubscriptionCycleName(c.Name, c.Seconds), Seconds: c.Seconds, WindowEnd: timestamp(syncPath(item, c.ResetPath)), SampledAt: sampledAt, Source: source}
			value, valid := syncPath(item, c.UtilizationPath).(float64)
			if c.Scale == "percent" {
				value /= 100
			}
			if valid && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100 {
				cycle.Utilization = &value
			}
			cycle.WindowStart = cycle.WindowEnd - c.Seconds
			switch {
			case cycle.Utilization == nil:
				cycle.IssueCode = "invalid_utilization"
			case cycle.WindowEnd == 0:
				cycle.IssueCode = "invalid_reset"
				if syncPath(item, c.ResetPath) == nil {
					cycle.IssueCode = "missing_reset"
				}
			case cycle.WindowStart <= 0 || cycle.WindowStart > now:
				cycle.IssueCode = "invalid_window"
			case cycle.WindowEnd <= now:
				cycle.IssueCode = "expired_reset"
			case cycle.SampledAt > now+60:
				cycle.IssueCode = "invalid_sample"
			case quality != "hint" && (cycle.SampledAt <= 0 || cycle.SampledAt < cycle.WindowStart || cycle.SampledAt >= cycle.WindowEnd):
				cycle.IssueCode = "invalid_sample"
			}
			if cycle.IssueCode != "" {
				cycle.Error = "Missing or inconsistent upstream cycle data"
			} else if quality == "hint" {
				cycle.IssueCode = "sample_time_incomplete"
			}
			account.Cycles = append(account.Cycles, cycle)
		}
		accounts = append(accounts, account)
	}
	return model.ReportedSubscriptionSyncAccounts(accounts), nil
}

type subscriptionSyncRateLimitError struct{ RetryAfter int64 }

func (*subscriptionSyncRateLimitError) Error() string {
	return "Synchronization upstream rate limit reached"
}
