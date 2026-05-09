package handlers

import (
	"fmt"
	"goscraper/src/globals"
	"strings"

	"github.com/valyala/fasthttp"
)

// spPostForm POSTs an x-www-form-urlencoded body to an authenticated SP
// endpoint, mimicking the browser headers the JSP front-end sends. It tracks
// rotated cookies (F5 WAF) and returns the response body plus the
// possibly-updated cookie string. If the response is a login bounceback the
// session is reported as expired.
func spPostForm(targetURL, formBody, spCookies string) (body []byte, newCookies string, expired bool, err error) {
	if spCookies == "" {
		return nil, "", true, fmt.Errorf("sp post: empty cookies")
	}

	jar := parseCookieString(spCookies)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(targetURL)
	req.Header.SetMethod("POST")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Accept", "text/html, */*; q=0.01")
	req.Header.Set("User-Agent", spUserAgent)
	req.Header.Set("Origin", spBaseURL)
	req.Header.Set("Referer", "https://sp.srmist.edu.in/srmiststudentportal/students/template/HRDSystem.jsp")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Cookie", spCookies)
	req.SetBodyString(formBody)

	if err := globals.HttpClient.DoRedirects(req, resp, 5); err != nil {
		return nil, "", false, fmt.Errorf("sp post: %w", err)
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

	respBody := append([]byte(nil), resp.Body()...)
	bodyLower := strings.ToLower(string(respBody))
	if strings.Contains(bodyLower, "youlogin.jsp") ||
		strings.Contains(bodyLower, `id="login_form"`) ||
		(strings.Contains(bodyLower, `name="username"`) && strings.Contains(bodyLower, `name="captcha"`)) {
		expired = true
	}

	if rotated {
		newCookies = serializeJar(jar)
	}
	return respBody, newCookies, expired, nil
}
