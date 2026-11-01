// Package basictoken implements the ordinary Aliyun Drive QR login flow.
package basictoken

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

const sessionTTL = 5 * time.Minute

type loginSession struct {
	client        *http.Client
	ck, timestamp string
	expires       time.Time
	poll          sync.Mutex
	timer         *time.Timer
}

// Service keeps only short-lived, unconfirmed login challenges in memory.
// Refresh/access tokens are never retained in the session store.
type Service struct {
	upstream    string
	mu          sync.Mutex
	sessions    map[string]*loginSession
	now         func() time.Time
	maxSessions int
	ttl         time.Duration
}

func New() *Service { return newService("https://passport.aliyundrive.com") }

func newService(upstream string) *Service {
	return &Service{upstream: upstream, sessions: make(map[string]*loginSession), now: time.Now, maxSessions: 128, ttl: sessionTTL}
}

func reply(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, code int, message string) {
	reply(w, code, map[string]string{"error": message})
}

func validRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, http.StatusMethodNotAllowed, "请使用POST请求")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Host, r.Host) {
			fail(w, http.StatusForbidden, "请在本站页面获取令牌")
			return false
		}
	}
	return true
}

func (s *Service) remove(id string) {
	s.mu.Lock()
	if session := s.sessions[id]; session != nil && session.timer != nil {
		session.timer.Stop()
	}
	delete(s.sessions, id)
	s.mu.Unlock()
}

func commonForm() url.Values {
	return url.Values{"appName": {"aliyun_drive"}, "fromSite": {"52"}, "appEntrance": {"web"}, "isMobile": {"false"}, "lang": {"zh_CN"}, "returnUrl": {""}, "bizParams": {""}, "_bx-v": {"2.0.31"}}
}

type upstreamReply struct {
	Content struct {
		Success bool `json:"success"`
		Data    struct {
			CodeContent string          `json:"codeContent"`
			CK          string          `json:"ck"`
			T           json.RawMessage `json:"t"`
			Status      string          `json:"qrCodeStatus"`
			BizExt      string          `json:"bizExt"`
		} `json:"data"`
	} `json:"content"`
}

func (s *Service) call(ctx context.Context, client *http.Client, path, method string, form url.Values) (*upstreamReply, error) {
	endpoint := s.upstream + path
	var body io.Reader
	if method == http.MethodGet {
		endpoint += "?" + form.Encode()
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.alipan.com/")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("upstream HTTP error")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("invalid upstream body")
	}
	var result upstreamReply
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if !result.Content.Success {
		return nil, errors.New("login request refused")
	}
	return &result, nil
}

// Generate creates a QR image locally; no QR contents are sent to an image service.
func (s *Service) Generate(w http.ResponseWriter, r *http.Request) {
	if !validRequest(w, r) {
		return
	}
	idBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		fail(w, 500, "暂时无法创建扫码会话")
		return
	}
	id := hex.EncodeToString(idBytes)
	jar, _ := cookiejar.New(nil)
	session := &loginSession{client: &http.Client{Jar: jar, Timeout: 12 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, expires: s.now().Add(s.ttl)}
	s.mu.Lock()
	for key, existing := range s.sessions {
		if !s.now().Before(existing.expires) {
			if existing.timer != nil {
				existing.timer.Stop()
			}
			delete(s.sessions, key)
		}
	}
	if len(s.sessions) >= s.maxSessions {
		s.mu.Unlock()
		fail(w, 429, "扫码会话较多，请稍后再试")
		return
	}
	session.timer = time.AfterFunc(s.ttl, func() { s.remove(id) })
	s.sessions[id] = session
	s.mu.Unlock()
	ok := false
	defer func() {
		if !ok {
			s.remove(id)
		}
	}()
	result, err := s.call(r.Context(), session.client, "/newlogin/qrcode/generate.do", http.MethodGet, commonForm())
	if err != nil {
		fail(w, 502, "阿里云盘暂时无法生成二维码，请稍后重试")
		return
	}
	data := result.Content.Data
	stamp := strings.Trim(string(data.T), `"`)
	if _, err := strconv.ParseInt(stamp, 10, 64); err != nil || data.CK == "" || len(data.CK) > 4096 || data.CodeContent == "" || len(data.CodeContent) > 8192 {
		fail(w, 502, "阿里云盘返回的二维码数据不完整")
		return
	}
	png, err := qrcode.Encode(data.CodeContent, qrcode.Medium, 280)
	if err != nil {
		fail(w, 502, "暂时无法绘制二维码，请重试")
		return
	}
	s.mu.Lock()
	session.ck, session.timestamp = data.CK, stamp
	s.mu.Unlock()
	ok = true
	reply(w, 200, map[string]any{"session_id": id, "qr_link": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), "expires_in": int(s.ttl.Seconds())})
}

func (s *Service) Check(w http.ResponseWriter, r *http.Request) {
	if !validRequest(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		SessionID string `json:"session_id"`
	}
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&input) != nil || input.SessionID == "" || len(input.SessionID) > 128 {
		fail(w, 400, "扫码会话参数无效")
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		fail(w, 400, "扫码会话参数无效")
		return
	}
	s.mu.Lock()
	session := s.sessions[input.SessionID]
	s.mu.Unlock()
	if session == nil {
		fail(w, 410, "二维码已失效，请重新生成")
		return
	}
	session.poll.Lock()
	defer session.poll.Unlock()
	// A second in-flight poll must not retrieve a consumed login result.
	s.mu.Lock()
	current := s.sessions[input.SessionID] == session && s.now().Before(session.expires)
	s.mu.Unlock()
	if !current {
		s.remove(input.SessionID)
		fail(w, 410, "二维码已失效，请重新生成")
		return
	}
	form := commonForm()
	form.Set("ck", session.ck)
	form.Set("t", session.timestamp)
	form.Set("navlanguage", "zh-CN")
	form.Set("navPlatform", "Win32")
	result, err := s.call(r.Context(), session.client, "/newlogin/qrcode/query.do", http.MethodPost, form)
	if err != nil {
		fail(w, 502, "阿里云盘暂时无法查询扫码状态，请重新生成二维码")
		return
	}
	data := result.Content.Data
	switch data.Status {
	case "NEW", "SCANED":
		reply(w, 200, map[string]string{"status": data.Status})
	case "EXPIRED", "CANCELED":
		s.remove(input.SessionID)
		reply(w, 200, map[string]string{"status": data.Status})
	case "CONFIRMED":
		s.remove(input.SessionID)
		plain, err := base64.StdEncoding.DecodeString(data.BizExt)
		if err != nil {
			plain, err = base64.RawStdEncoding.DecodeString(data.BizExt)
		}
		var login struct {
			Result struct {
				RefreshToken string `json:"refreshToken"`
			} `json:"pds_login_result"`
		}
		if err != nil || json.Unmarshal(plain, &login) != nil || login.Result.RefreshToken == "" || len(login.Result.RefreshToken) > 8192 {
			fail(w, 502, "登录已确认，但未取得普通刷新令牌，请重新扫码")
			return
		}
		reply(w, 200, map[string]any{"status": "CONFIRMED", "refresh_token": login.Result.RefreshToken, "token_length": len(login.Result.RefreshToken)})
	default:
		fail(w, 502, "阿里云盘返回了未知扫码状态，请重新生成二维码")
	}
}
