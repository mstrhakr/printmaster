package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"printmaster/common/logger"
)

// VendorLoginAdapter handles vendor-specific login flows for Web UI auto-login.
type VendorLoginAdapter interface {
	// Login performs the login flow and returns a cookie jar with session cookies.
	Login(baseURL, username, password string, log *logger.Logger) (*cookiejar.Jar, error)
	// Name returns the vendor name (e.g., "Epson", "Kyocera").
	Name() string
}

// SessionCache stores active login sessions per device serial.
type SessionCache struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntry
}

type sessionEntry struct {
	Jar       *cookiejar.Jar
	ExpiresAt time.Time
}

// NewSessionCache creates a new session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{sessions: make(map[string]*sessionEntry)}
}

// Get retrieves a session jar if valid, otherwise returns nil.
func (sc *SessionCache) Get(serial string) *cookiejar.Jar {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if e, ok := sc.sessions[serial]; ok && time.Now().Before(e.ExpiresAt) {
		return e.Jar
	}
	return nil
}

// Set stores a session jar with a 15-minute expiration.
func (sc *SessionCache) Set(serial string, jar *cookiejar.Jar) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.sessions[serial] = &sessionEntry{
		Jar:       jar,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
}

// Clear removes a session from the cache.
func (sc *SessionCache) Clear(serial string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.sessions, serial)
}

// EpsonLoginAdapter handles Epson printer login flows.
type EpsonLoginAdapter struct{}

func (e *EpsonLoginAdapter) Name() string { return "Epson" }

func (e *EpsonLoginAdapter) Login(baseURL, username, password string, log *logger.Logger) (*cookiejar.Jar, error) {
	log.Debug("Epson login attempt", "base_url", baseURL, "username", username)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec G402 -- InsecureSkipVerify intentionally enabled:
				// Network printers commonly use self-signed SSL certificates.
				// This adapter authenticates to printer web interfaces on local networks.
				InsecureSkipVerify: true,
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow redirects but keep cookies
			return nil
		},
	}

	// Step 1: GET the login page to establish session
	// Try advanced password page first (newer models), fallback to top index
	loginPagePaths := []string{
		"/PRESENTATION/ADVANCED/PASSWORD",
		"/PRESENTATION/HTML/TOP/INDEX.HTML",
	}

	var loginPageURL string
	var pageBody string
	for _, path := range loginPagePaths {
		log.Debug("Epson login: trying path", "path", path)
		resp, err := client.Get(baseURL + path)
		if err != nil {
			log.Debug("Epson login: GET failed", "path", path, "error", err.Error())
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			loginPageURL = baseURL + path
			pageBody = string(body)
			log.Debug("Epson login: got login page", "url", loginPageURL, "body_length", len(pageBody))
			break
		}
		log.Debug("Epson login: unexpected status", "path", path, "status", resp.StatusCode)
	}

	if loginPageURL == "" {
		log.Warn("Epson login: could not fetch any login page")
		return nil, fmt.Errorf("could not fetch login page")
	}

	// Step 2: Determine form action from the page
	// Look for <form action="SET" or action="LOGIN.HTML"
	formAction := "SET" // Default for newer models
	if idx := strings.Index(pageBody, `action="`); idx >= 0 {
		start := idx + 8
		if end := strings.Index(pageBody[start:], `"`); end >= 0 {
			formAction = pageBody[start : start+end]
		}
	}
	log.Debug("Epson login: form action detected", "action", formAction)

	// Step 3: POST credentials using the correct field names
	form := url.Values{}
	form.Set("INPUTT_USERNAME", username) // Advanced password page format
	form.Set("INPUTT_PASSWORD", password)
	form.Set("SEL_SESSIONTYPE", "ADMIN")
	form.Set("access", "https") // Optional security field

	// Construct POST URL - formAction is relative to login page directory
	postURL := loginPageURL
	if !strings.HasSuffix(postURL, "/") {
		postURL += "/"
	}
	postURL += formAction
	log.Debug("Epson login: POSTing credentials", "url", postURL)

	postResp, err := client.PostForm(postURL, form)
	if err != nil {
		log.Warn("Epson login: POST failed", "url", postURL, "error", err.Error())
		return nil, fmt.Errorf("login POST failed: %w", err)
	}
	defer postResp.Body.Close()

	// Check if login was successful (Epson redirects or returns 200)
	log.Debug("Epson login: response received", "status", postResp.StatusCode, "cookies_count", len(jar.Cookies(postResp.Request.URL)))
	if postResp.StatusCode == http.StatusOK || postResp.StatusCode == http.StatusFound || postResp.StatusCode == http.StatusSeeOther {
		log.Info("Epson login: SUCCESS", "status", postResp.StatusCode, "base_url", baseURL)
		return jar, nil
	}

	log.Warn("Epson login: FAILED", "status", postResp.StatusCode, "base_url", baseURL)
	return nil, fmt.Errorf("login failed: status %d", postResp.StatusCode)
}

// KyoceraLoginAdapter handles Kyocera printer login flows.
type KyoceraLoginAdapter struct{}

func (k *KyoceraLoginAdapter) Name() string { return "Kyocera" }

func (k *KyoceraLoginAdapter) Login(baseURL, username, password string, log *logger.Logger) (*cookiejar.Jar, error) {
	log.Debug("Kyocera login attempt", "base_url", baseURL, "username", username)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec G402 -- InsecureSkipVerify intentionally enabled:
				// Network printers commonly use self-signed SSL certificates.
				// This adapter authenticates to printer web interfaces on local networks.
				InsecureSkipVerify: true,
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Follow redirects but keep cookies
			return nil
		},
	}

	// Kyocera Command Center RX login paths to try
	loginPaths := []string{
		"/wlmeng/index.htm",
		"/startwlm/Start_Wlm.htm",
		"/startpage.htm",
	}

	var loginPageURL string
	var loginPageBody string

	// Step 1: Try to GET the login page
	for _, path := range loginPaths {
		log.Debug("Kyocera login: trying path", "path", path)
		resp, err := client.Get(baseURL + path)
		if err != nil {
			log.Debug("Kyocera login: GET failed", "path", path, "error", err.Error())
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			loginPageBody = string(body)
			loginPageURL = resp.Request.URL.String()
			log.Debug("Kyocera login: got login page", "url", loginPageURL, "body_length", len(loginPageBody))
			break
		}
	}

	if loginPageURL == "" {
		log.Warn("Kyocera login: could not fetch any login page")
		return nil, fmt.Errorf("could not fetch login page")
	}

	// Step 2: Parse hidden fields from the form
	hiddenFields := parseHiddenFields(loginPageBody)
	log.Debug("Kyocera login: parsed hidden fields", "count", len(hiddenFields))

	// Step 3: POST credentials to the same page (form action is usually empty or same page)
	form := url.Values{}
	form.Set("arg01_UserName", username) // Kyocera uses arg01_UserName
	form.Set("arg02_Password", password) // Kyocera uses arg02_Password
	form.Set("Login", "Login")           // Submit button value

	// Add all hidden fields from the form
	for k, v := range hiddenFields {
		form.Set(k, v)
		log.Debug("Kyocera login: adding hidden field", "name", k, "value_length", len(v))
	}

	log.Debug("Kyocera login: POSTing credentials", "url", loginPageURL)
	postResp, err := client.PostForm(loginPageURL, form)
	if err != nil {
		log.Warn("Kyocera login: POST failed", "error", err.Error())
		return nil, fmt.Errorf("login POST failed: %w", err)
	}
	defer postResp.Body.Close()

	// Check if we got cookies
	parsedURL, _ := url.Parse(loginPageURL)
	cookies := jar.Cookies(parsedURL)
	log.Debug("Kyocera login: response received", "status", postResp.StatusCode, "cookies_count", len(cookies))

	// Kyocera typically returns 200 on success
	if postResp.StatusCode == http.StatusOK || postResp.StatusCode == http.StatusFound || postResp.StatusCode == http.StatusSeeOther {
		log.Info("Kyocera login: SUCCESS", "status", postResp.StatusCode, "base_url", baseURL, "cookies_received", len(cookies))
		return jar, nil
	}

	log.Warn("Kyocera login: FAILED", "status", postResp.StatusCode, "base_url", baseURL)
	return nil, fmt.Errorf("login failed: status %d", postResp.StatusCode)
}

// parseHiddenFields extracts <input type="hidden"> fields from HTML.
func parseHiddenFields(html string) map[string]string {
	fields := make(map[string]string)
	// Match: <input type="hidden" name="X" value="Y">
	re := regexp.MustCompile(`(?i)<input[^>]*type=["']?hidden["']?[^>]*name=["']?([^"'\s>]+)["']?[^>]*value=["']?([^"'\s>]*)["']?`)
	matches := re.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			fields[m[1]] = m[2]
		}
	}
	return fields
}

// GetAdapterForManufacturer returns the appropriate login adapter for a given manufacturer.
func GetAdapterForManufacturer(manufacturer string) VendorLoginAdapter {
	mfgLower := strings.ToLower(manufacturer)
	switch {
	case strings.Contains(mfgLower, "epson"):
		return &EpsonLoginAdapter{}
	case strings.Contains(mfgLower, "kyocera"):
		return &KyoceraLoginAdapter{}
	default:
		return nil
	}
}
