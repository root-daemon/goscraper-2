package handlers

import (
	"fmt"
	"goscraper/src/globals"
	"strings"

	"github.com/valyala/fasthttp"
)

const spDashboardURL = "https://sp.srmist.edu.in/srmiststudentportal/students/template/HRDSystem.jsp"

type SPProbeResult struct {
	Status        int    `json:"status"`
	Authenticated bool   `json:"authenticated"`
	BodyLength    int    `json:"bodyLength"`
	Snippet       string `json:"snippet"`
	FinalURL      string `json:"finalUrl"`
	// NewCookies is set when the F5 WAF cookie rotates mid-session — the
	// caller should send this back to the client as X-Updated-SP-Token.
	NewCookies string `json:"-"`
}

func SPProbe(spCookies string) (*SPProbeResult, error) {
	if spCookies == "" {
		return nil, fmt.Errorf("sp probe: empty cookies")
	}

	jar := parseCookieString(spCookies)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(spDashboardURL)
	req.Header.SetMethod("GET")
	req.Header.Set("User-Agent", spUserAgent)
	req.Header.Set("Referer", spLoginPageURL)
	req.Header.Set("Cookie", spCookies)

	if err := globals.HttpClient.DoRedirects(req, resp, 5); err != nil {
		return nil, fmt.Errorf("sp probe: %w", err)
	}

	rotated := false
	resp.Header.VisitAllCookie(func(key, value []byte) {
		c := fasthttp.AcquireCookie()
		defer fasthttp.ReleaseCookie(c)
		c.ParseBytes(value)
		val := string(c.Value())
		if val == "" || val == "delete" || val == "null" {
			return
		}
		k := string(key)
		if jar[k] != val {
			jar[k] = val
			rotated = true
		}
	})

	body := string(resp.Body())
	bodyLower := strings.ToLower(body)
	loggedOut := strings.Contains(bodyLower, "youlogin.jsp") ||
		strings.Contains(bodyLower, `id="login_form"`) ||
		(strings.Contains(bodyLower, `name="username"`) && strings.Contains(bodyLower, `name="captcha"`))

	snippet := body
	if len(snippet) > 400 {
		snippet = snippet[:400]
	}

	result := &SPProbeResult{
		Status:        resp.StatusCode(),
		Authenticated: !loggedOut && resp.StatusCode() < 400,
		BodyLength:    len(body),
		Snippet:       snippet,
		FinalURL:      string(resp.Header.Peek("Location")),
	}
	if rotated {
		result.NewCookies = serializeJar(jar)
	}
	return result, nil
}

func parseCookieString(s string) map[string]string {
	jar := make(map[string]string)
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		if eq <= 0 {
			continue
		}
		jar[p[:eq]] = p[eq+1:]
	}
	return jar
}

func serializeJar(jar map[string]string) string {
	parts := make([]string, 0, len(jar))
	for k, v := range jar {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}
