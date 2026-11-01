package basictoken

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fakeRefresh = "0123456789abcdef0123456789abcdef"

func fixture(t *testing.T, status, biz string) *Service {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/newlogin/qrcode/generate.do":
			if r.URL.Query().Get("appName") != "aliyun_drive" || r.URL.Query().Get("fromSite") != "52" {
				t.Error("ordinary login parameters missing")
			}
			http.SetCookie(w, &http.Cookie{Name: "qr_test", Value: "cookie-test", Path: "/"})
			fmt.Fprint(w, `{"content":{"success":true,"data":{"codeContent":"https://www.alipan.com/test-qr","ck":"private-challenge","t":123456}}}`)
		case "/newlogin/qrcode/query.do":
			if r.Method != http.MethodPost {
				t.Error("polling must POST")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("ck") != "private-challenge" || r.Form.Get("t") != "123456" || r.Form.Get("appName") != "aliyun_drive" {
				t.Error("wrong polling form")
			}
			cookie, err := r.Cookie("qr_test")
			if err != nil || cookie.Value != "cookie-test" {
				t.Error("QR session cookies were lost")
			}
			json.NewEncoder(w).Encode(map[string]any{"content": map[string]any{"success": true, "data": map[string]any{"qrCodeStatus": status, "bizExt": biz}}})
		default:
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(upstream.Close)
	return newService(upstream.URL)
}

func request(handler http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, "/basic/test", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	handler(w, r)
	return w
}

func generate(t *testing.T, s *Service) string {
	t.Helper()
	w := request(s.Generate, http.MethodPost, "")
	if w.Code != 200 {
		t.Fatalf("generate: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		SessionID string `json:"session_id"`
		QRLink    string `json:"qr_link"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.SessionID) < 32 {
		t.Fatal("missing unpredictable session ID")
	}
	if !strings.HasPrefix(result.QRLink, "data:image/png;base64,") {
		t.Fatal("QR must be generated locally")
	}
	for _, unwanted := range []string{"private-challenge", "cookie-test", "123456"} {
		if strings.Contains(w.Body.String(), unwanted) {
			t.Errorf("upstream login state leaked: %s", unwanted)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("login response must not be cached")
	}
	return result.SessionID
}

func check(s *Service, session string) *httptest.ResponseRecorder {
	return request(s.Check, http.MethodPost, `{"session_id":"`+session+`"}`)
}

func TestOrdinaryLoginOnlyReturnsRefreshToken(t *testing.T) {
	biz := base64.StdEncoding.EncodeToString([]byte(`{"pds_login_result":{"refreshToken":"` + fakeRefresh + `","accessToken":"DO_NOT_EXPOSE_ACCESS","nickName":"DO_NOT_EXPOSE_PROFILE"}}`))
	s := fixture(t, "CONFIRMED", biz)
	sid := generate(t, s)
	w := check(s, sid)
	if w.Code != 200 {
		t.Fatalf("check status=%d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "CONFIRMED" || body["refresh_token"] != fakeRefresh || body["token_length"] != float64(32) {
		t.Fatalf("wrong token result: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "DO_NOT_EXPOSE") || strings.Contains(w.Body.String(), "bizExt") {
		t.Fatal("complete login response leaked")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("token response must not be cached")
	}
	if again := check(s, sid); again.Code != http.StatusGone {
		t.Errorf("successful session must be consumed, got %d", again.Code)
	}
}

func TestWaitingAndTerminalStatuses(t *testing.T) {
	for _, status := range []string{"NEW", "SCANED", "EXPIRED", "CANCELED"} {
		t.Run(status, func(t *testing.T) {
			s := fixture(t, status, "")
			sid := generate(t, s)
			w := check(s, sid)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"`+status+`"`) {
				t.Fatalf("wrong status: %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "refresh_token") {
				t.Error("token returned before confirmation")
			}
			if status == "EXPIRED" || status == "CANCELED" {
				if again := check(s, sid); again.Code != 410 {
					t.Error("terminal session retained")
				}
			}
		})
	}
}

func TestMalformedLoginResultFailsWithoutLeakingUpstream(t *testing.T) {
	for _, biz := range []string{"invalid-base64", base64.StdEncoding.EncodeToString([]byte(`{"pds_login_result":{"accessToken":"DO_NOT_EXPOSE"}}`))} {
		s := fixture(t, "CONFIRMED", biz)
		w := check(s, generate(t, s))
		if w.Code != http.StatusBadGateway {
			t.Errorf("malformed confirmation accepted: %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "DO_NOT_EXPOSE") || strings.Contains(w.Body.String(), biz) {
			t.Error("raw upstream data leaked")
		}
	}
}

func TestInvalidRequestsNeverReachUpstream(t *testing.T) {
	var calls atomic.Int32
	u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer u.Close()
	s := newService(u.URL)
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{`, 400}, {`{}`, 400}, {`{"session_id":"unknown"}`, 410},
		{`{"session_id":"` + strings.Repeat("x", 5000) + `"}`, 400},
		{`{"session_id":"unknown"} {}`, 400},
	} {
		w := request(s.Check, "POST", tc.body)
		if w.Code != tc.code {
			t.Errorf("expected %d, got %d", tc.code, w.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request reached login server")
	}
}

func TestUpstreamFailuresAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
	}{
		{503, "DO_NOT_EXPOSE"}, {200, "<html>DO_NOT_EXPOSE</html>"},
		{200, `{"content":{"success":false,"data":{"codeContent":"DO_NOT_EXPOSE"}}}`},
		{200, `{"content":{"success":true,"data":{}}}`},
	} {
		u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); io.WriteString(w, tc.body) }))
		s := newService(u.URL)
		w := request(s.Generate, "POST", "")
		u.Close()
		if w.Code != 502 {
			t.Errorf("expected upstream error, got %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "DO_NOT_EXPOSE") {
			t.Error("raw upstream error leaked")
		}
	}
}

func TestExpiredSessionsReleaseCapacity(t *testing.T) {
	s := fixture(t, "NEW", "")
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.maxSessions = 1
	sid := generate(t, s)
	if w := request(s.Generate, "POST", ""); w.Code != 429 {
		t.Fatalf("unbounded sessions: %d", w.Code)
	}
	now = now.Add(6 * time.Minute)
	if w := check(s, sid); w.Code != 410 {
		t.Errorf("expired session allowed: %d", w.Code)
	}
	generate(t, s)
}

func TestIdleExpiredSessionsAreActuallyRemoved(t *testing.T) {
	s := fixture(t, "NEW", "")
	s.ttl = 100 * time.Millisecond
	generate(t, s)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		remaining := len(s.sessions)
		s.mu.Unlock()
		if remaining == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expired login cookies retained without another request")
}

func TestTokenIsNeverTruncatedTo32Characters(t *testing.T) {
	token := strings.Repeat("synthetic-token-", 5)
	biz := base64.StdEncoding.EncodeToString([]byte(`{"pds_login_result":{"refreshToken":"` + token + `"}}`))
	s := fixture(t, "CONFIRMED", biz)
	w := check(s, generate(t, s))
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != 200 || body["refresh_token"] != token || body["token_length"] != float64(80) {
		// "synthetic-token-" is 16 characters; signed credentials must remain intact.
		t.Errorf("token was modified: %s", w.Body.String())
	}
}

func TestMethodsAndForeignOriginsAreRejected(t *testing.T) {
	s := newService("http://127.0.0.1:1")
	for _, handler := range []http.HandlerFunc{s.Generate, s.Check} {
		if w := request(handler, "GET", ""); w.Code != 405 {
			t.Errorf("unexpected method accepted: %d", w.Code)
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "http://service.test/basic/test", strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://unrelated.test")
		handler(w, r)
		if w.Code != 403 {
			t.Errorf("foreign origin accepted: %d", w.Code)
		}
	}
}

func TestIndependentLoginSessionsDoNotShareCookiesOrTokens(t *testing.T) {
	var counter atomic.Int32
	u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "generate.do") {
			id := counter.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "login_id", Value: fmt.Sprint(id), Path: "/"})
			fmt.Fprintf(w, `{"content":{"success":true,"data":{"codeContent":"https://www.alipan.com/test","ck":"%d","t":123}}}`, id)
			return
		}
		r.ParseForm()
		cookie, err := r.Cookie("login_id")
		if err != nil || cookie.Value != r.Form.Get("ck") {
			t.Error("sessions mixed")
			w.WriteHeader(400)
			return
		}
		biz := base64.StdEncoding.EncodeToString([]byte(`{"pds_login_result":{"refreshToken":"synthetic-session-` + cookie.Value + `"}}`))
		fmt.Fprintf(w, `{"content":{"success":true,"data":{"qrCodeStatus":"CONFIRMED","bizExt":"%s"}}}`, biz)
	}))
	defer u.Close()
	s := newService(u.URL)
	first, second := generate(t, s), generate(t, s)
	if first == second {
		t.Fatal("session IDs reused")
	}
	for _, tc := range []struct{ id, want string }{{second, "synthetic-session-2"}, {first, "synthetic-session-1"}} {
		w := check(s, tc.id)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"refresh_token":"`+tc.want+`"`) {
			t.Fatalf("wrong account token: %s", w.Body.String())
		}
	}
}
