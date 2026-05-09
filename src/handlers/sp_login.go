package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"goscraper/src/globals"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/valyala/fasthttp"
)

const (
	spLoginPageURL = "https://sp.srmist.edu.in/srmiststudentportal/students/loginManager/youLogin.jsp"
	spLoginPostURL = "https://sp.srmist.edu.in/srmiststudentportal/SLoginServlet"
	spBaseURL      = "https://sp.srmist.edu.in"

	// Server may bind fingerprint at login and check on subsequent requests, so the same
	// User-Agent is used for every SP request and the fingerprint string is fixed.
	spUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	spFingerprint = spUserAgent + "1920" + "1080" + "en-US" + "MacIntel" + "24" + "Asia/Kolkata" + "8"
)

type SPLoginFetcher struct{}

type SPCaptchaData struct {
	Image string `json:"image"`
	State string `json:"state"`
}

type SPLoginResult struct {
	Authenticated bool   `json:"authenticated"`
	Cookies       string `json:"cookies,omitempty"`
	Status        int    `json:"status"`
	Message       string `json:"message,omitempty"`
}

type spLoginState struct {
	JSessionID  string `json:"j"`
	TSCookieKey string `json:"tk"`
	TSCookieVal string `json:"tv"`
	CSRFToken   string `json:"c"`
	JSChallenge string `json:"jc"`
}

func (sp *SPLoginFetcher) Init() (*SPCaptchaData, error) {
	jar := make(map[string]string)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(spLoginPageURL)
	req.Header.SetMethod("GET")
	req.Header.Set("User-Agent", spUserAgent)

	if err := globals.HttpClient.Do(req, resp); err != nil {
		return nil, fmt.Errorf("sp init: fetch login page: %w", err)
	}
	sp.extractCookies(resp, jar)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(resp.Body())))
	if err != nil {
		return nil, fmt.Errorf("sp init: parse html: %w", err)
	}

	csrfToken, _ := doc.Find("input[name=csrfToken]").Attr("value")
	jsChallenge, _ := doc.Find("input[name=jsChallenge]").Attr("value")
	captchaSrc, _ := doc.Find("img[alt=Captcha]").Attr("src")

	if csrfToken == "" || jsChallenge == "" || captchaSrc == "" {
		return nil, fmt.Errorf("sp init: missing form fields (csrf=%t jsc=%t captcha=%t)",
			csrfToken != "", jsChallenge != "", captchaSrc != "")
	}

	captchaURL := captchaSrc
	if strings.HasPrefix(captchaSrc, "/") {
		captchaURL = spBaseURL + captchaSrc
	} else if !strings.HasPrefix(captchaSrc, "http") {
		captchaURL = spBaseURL + "/" + captchaSrc
	}

	capReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(capReq)
	capResp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(capResp)

	capReq.SetRequestURI(captchaURL)
	capReq.Header.SetMethod("GET")
	capReq.Header.Set("User-Agent", spUserAgent)
	capReq.Header.Set("Referer", spLoginPageURL)
	capReq.Header.Set("Cookie", sp.cookieStr(jar))

	if err := globals.HttpClient.Do(capReq, capResp); err != nil {
		return nil, fmt.Errorf("sp init: fetch captcha: %w", err)
	}
	sp.extractCookies(capResp, jar)

	contentType := string(capResp.Header.ContentType())
	if contentType == "" {
		contentType = "image/png"
	}
	imgB64 := base64.StdEncoding.EncodeToString(capResp.Body())
	dataURL := fmt.Sprintf("data:%s;base64,%s", contentType, imgB64)

	var tsKey, tsVal string
	for k, v := range jar {
		if strings.HasPrefix(k, "TS") {
			tsKey, tsVal = k, v
			break
		}
	}

	stateBlob, err := encodeSPState(&spLoginState{
		JSessionID:  jar["JSESSIONID"],
		TSCookieKey: tsKey,
		TSCookieVal: tsVal,
		CSRFToken:   csrfToken,
		JSChallenge: jsChallenge,
	})
	if err != nil {
		return nil, fmt.Errorf("sp init: encode state: %w", err)
	}

	return &SPCaptchaData{Image: dataURL, State: stateBlob}, nil
}

func (sp *SPLoginFetcher) Complete(username, password, captcha, encodedState string) (*SPLoginResult, error) {
	state, err := decodeSPState(encodedState)
	if err != nil {
		return nil, fmt.Errorf("sp complete: decode state: %w", err)
	}
	if state.JSessionID == "" || state.JSChallenge == "" || state.CSRFToken == "" {
		return &SPLoginResult{Authenticated: false, Status: 400, Message: "Invalid SP state"}, nil
	}

	jar := map[string]string{"JSESSIONID": state.JSessionID}
	if state.TSCookieKey != "" {
		jar[state.TSCookieKey] = state.TSCookieVal
	}

	jsResponse := base64.StdEncoding.EncodeToString([]byte(state.JSChallenge + spFingerprint))

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(spLoginPostURL)
	req.Header.SetMethod("POST")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", spUserAgent)
	req.Header.Set("Origin", spBaseURL)
	req.Header.Set("Referer", spLoginPageURL)
	req.Header.Set("Cookie", sp.cookieStr(jar))

	args := fasthttp.AcquireArgs()
	defer fasthttp.ReleaseArgs(args)
	args.Add("username", username)
	args.Add("password", password)
	args.Add("captcha", captcha)
	args.Add("txtPageAction", "0")
	args.Add("csrfToken", state.CSRFToken)
	args.Add("jsChallenge", state.JSChallenge)
	args.Add("jsResponse", jsResponse)
	args.Add("fingerprint", spFingerprint)
	args.Add("netId", "")
	req.SetBody(args.QueryString())

	if err := globals.HttpClient.DoRedirects(req, resp, 10); err != nil {
		return nil, fmt.Errorf("sp complete: post: %w", err)
	}
	sp.extractCookies(resp, jar)

	body := strings.ToLower(string(resp.Body()))
	location := strings.ToLower(string(resp.Header.Peek("Location")))

	loginPageReturned := strings.Contains(body, "youlogin.jsp") ||
		strings.Contains(location, "youlogin.jsp") ||
		strings.Contains(body, `id="login_form"`)

	if loginPageReturned {
		msg := "Login failed"
		switch {
		case strings.Contains(body, "captcha"):
			msg = "Invalid captcha"
		case strings.Contains(body, "password") || strings.Contains(body, "credential"):
			msg = "Invalid credentials"
		case strings.Contains(body, "locked"):
			msg = "Account locked"
		}
		return &SPLoginResult{Authenticated: false, Status: 401, Message: msg}, nil
	}

	cookieStr := sp.cookieStr(jar)
	if !strings.Contains(cookieStr, "JSESSIONID") {
		return &SPLoginResult{Authenticated: false, Status: 401, Message: "SP session not established"}, nil
	}

	return &SPLoginResult{Authenticated: true, Cookies: cookieStr, Status: 200}, nil
}

func (sp *SPLoginFetcher) extractCookies(resp *fasthttp.Response, jar map[string]string) {
	resp.Header.VisitAllCookie(func(key, value []byte) {
		c := fasthttp.AcquireCookie()
		defer fasthttp.ReleaseCookie(c)
		c.ParseBytes(value)
		if val := string(c.Value()); val != "" && val != "delete" && val != "null" {
			jar[string(key)] = val
		}
	})
}

func (sp *SPLoginFetcher) cookieStr(jar map[string]string) string {
	parts := make([]string, 0, len(jar))
	for k, v := range jar {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, "; ")
}

func encodeSPState(s *spLoginState) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func decodeSPState(s string) (*spLoginState, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	var st spLoginState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
