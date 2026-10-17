package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"tgto/internal/db"
	"tgto/internal/logger"
)

// 天翼保活测试：用 httptest 顶替 tyAPIBase/tyAuthBase，验证
//   1) keepUserSession 成功即续期（updated_at 落盘、SessionKey 不变）
//   2) SessionKey 失效 → getSessionForPC 用 accessToken 换新会话
//   3) accessToken 也失效 → refreshToken.do 换 token 后再换会话

func newTyTestServices(t *testing.T) *Services {
	t.Helper()
	dir := t.TempDir()
	ring := logger.NewRingLogger(200)
	database, err := db.Open(dir, ring)
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return NewServices(database, ring, dir, dir, "test")
}

func withTyBases(t *testing.T, api, auth string) {
	t.Helper()
	oldAPI, oldAuth := tyAPIBase, tyAuthBase
	tyAPIBase, tyAuthBase = api, auth
	t.Cleanup(func() { tyAPIBase, tyAuthBase = oldAPI, oldAuth })
}

func seedTySession(t *testing.T, s *Services) {
	t.Helper()
	sess := &tySession{
		SessionKey:    "old-key",
		SessionSecret: "old-secret",
		AccessToken:   "old-at",
		RefreshToken:  "old-rt",
		LoginName:     "18900000000",
		UpdatedAt:     1,
	}
	if err := s.saveTySession(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}
}

func TestTyKeepAliveExtendsSession(t *testing.T) {
	s := newTyTestServices(t)
	seedTySession(t, s)

	var keepCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/keepUserSession.action" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		atomic.AddInt32(&keepCalls, 1)
		if r.Header.Get("SessionKey") != "old-key" {
			t.Errorf("缺少 SessionKey 头: %q", r.Header.Get("SessionKey"))
		}
		if r.Header.Get("Signature") == "" || r.Header.Get("Date") == "" {
			t.Errorf("缺少签名头")
		}
		if r.URL.Query().Get("clientType") != "TELEPC" {
			t.Errorf("缺少 clientType 参数: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"res_code":0}`))
	}))
	defer srv.Close()
	withTyBases(t, srv.URL, srv.URL)

	s.tyKeepAliveOnce(context.Background())

	if atomic.LoadInt32(&keepCalls) != 1 {
		t.Fatalf("keepUserSession 调用次数 = %d, want 1", keepCalls)
	}
	sess, err := s.loadTySession()
	if err != nil || sess == nil {
		t.Fatalf("load session: %v", err)
	}
	if sess.UpdatedAt <= 1 {
		t.Fatalf("updated_at 未刷新: %d", sess.UpdatedAt)
	}
	if sess.SessionKey != "old-key" || sess.SessionSecret != "old-secret" {
		t.Fatalf("保活不应改变会话: %+v", sess)
	}
}

func TestTyKeepAliveRefreshesExpiredSession(t *testing.T) {
	s := newTyTestServices(t)
	seedTySession(t, s)

	var sessionCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/keepUserSession.action":
			_, _ = w.Write([]byte(`{"res_code":-1,"res_message":"userSessionBO is null"}`))
		case "/getSessionForPC.action":
			atomic.AddInt32(&sessionCalls, 1)
			if r.URL.Query().Get("accessToken") != "old-at" {
				t.Errorf("accessToken 参数错误: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"res_code":0,"sessionKey":"new-key","sessionSecret":"new-secret","loginName":"18912345678","keepAlive":7200}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withTyBases(t, srv.URL, srv.URL)

	s.tyKeepAliveOnce(context.Background())

	if atomic.LoadInt32(&sessionCalls) != 1 {
		t.Fatalf("getSessionForPC 调用次数 = %d, want 1", sessionCalls)
	}
	sess, err := s.loadTySession()
	if err != nil || sess == nil {
		t.Fatalf("load session: %v", err)
	}
	if sess.SessionKey != "new-key" || sess.SessionSecret != "new-secret" {
		t.Fatalf("会话未刷新: %+v", sess)
	}
	if sess.KeepAlive != 7200 {
		t.Fatalf("keepAlive 未更新: %d", sess.KeepAlive)
	}
	if sess.AccessToken != "old-at" {
		t.Fatalf("accessToken 不应被改动: %s", sess.AccessToken)
	}
	if acct, _ := s.GetConfig(DriveTianYi)["account"].(string); acct != "18912345678" {
		t.Fatalf("账号名未同步到配置: %q", acct)
	}
}

func TestTyKeepAliveRefreshesAccessTokenWhenInvalid(t *testing.T) {
	s := newTyTestServices(t)
	seedTySession(t, s)

	var tokenCalls, sessionCalls int32
	var tokenRefreshed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/keepUserSession.action":
			_, _ = w.Write([]byte(`{"res_code":-1,"res_message":"userSessionBO is null"}`))
		case "/getSessionForPC.action":
			atomic.AddInt32(&sessionCalls, 1)
			if !tokenRefreshed.Load() {
				_, _ = w.Write([]byte(`{"res_code":1,"res_message":"UserInvalidOpenToken"}`))
				return
			}
			if r.URL.Query().Get("accessToken") != "new-at" {
				t.Errorf("刷新后应使用新 accessToken: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"res_code":0,"sessionKey":"fresh-key","sessionSecret":"fresh-secret","loginName":"18912345678"}`))
		case "/api/oauth2/refreshToken.do":
			atomic.AddInt32(&tokenCalls, 1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if r.PostForm.Get("refreshToken") != "old-rt" || r.PostForm.Get("grantType") != "refresh_token" {
				t.Errorf("refreshToken 表单错误: %v", r.PostForm)
			}
			tokenRefreshed.Store(true)
			_, _ = w.Write([]byte(`{"res_code":0,"accessToken":"new-at","refreshToken":"new-rt"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withTyBases(t, srv.URL, srv.URL)

	s.tyKeepAliveOnce(context.Background())

	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("refreshToken 调用次数 = %d, want 1", tokenCalls)
	}
	if atomic.LoadInt32(&sessionCalls) != 2 {
		t.Fatalf("getSessionForPC 调用次数 = %d, want 2（先失败再成功）", sessionCalls)
	}
	sess, err := s.loadTySession()
	if err != nil || sess == nil {
		t.Fatalf("load session: %v", err)
	}
	if sess.AccessToken != "new-at" || sess.RefreshToken != "new-rt" {
		t.Fatalf("token 未刷新: %+v", sess)
	}
	if sess.SessionKey != "fresh-key" || sess.SessionSecret != "fresh-secret" {
		t.Fatalf("会话未刷新: %+v", sess)
	}
}

func TestTyKeepAliveSkipsWhenLoggedOut(t *testing.T) {
	s := newTyTestServices(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"res_code":0}`))
	}))
	defer srv.Close()
	withTyBases(t, srv.URL, srv.URL)

	s.tyKeepAliveOnce(context.Background())

	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("未登录时不应发起请求，实际 %d 次", calls)
	}
}

func TestTyResCode(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{`{"res_code":0}`, 0, true},
		{`{"res_code":-10001}`, -10001, true},
		{`{"res_code":"12"}`, 12, true},
		{`{"other":1}`, 0, false},
	}
	for _, c := range cases {
		dec := tyDecodeBiz(c.in)
		got, ok := tyResCode(dec)
		if got != c.want || ok != c.ok {
			t.Fatalf("tyResCode(%s) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestTyKeepAliveMarksInvalidOnAuthFailure(t *testing.T) {
	s := newTyTestServices(t)
	seedTySession(t, s)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/keepUserSession.action":
			_, _ = w.Write([]byte(`{"res_code":-1,"res_message":"userSessionBO is null"}`))
		case "/getSessionForPC.action":
			// 线上真实返回：res_code 是字符串错误码
			_, _ = w.Write([]byte(`{"res_code":"UserInvalidOpenToken","res_message":"error in login get unifyAccountInfo is null"}`))
		case "/api/oauth2/refreshToken.do":
			// 线上真实返回：result/msg 字段
			_, _ = w.Write([]byte("{\"result\":-117,\"msg\":\"刷新失败\"}"))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withTyBases(t, srv.URL, srv.URL)

	s.tyKeepAliveOnce(context.Background())

	sess, err := s.loadTySession()
	if err != nil || sess == nil {
		t.Fatalf("load session: %v", err)
	}
	if !sess.Invalid {
		t.Fatalf("鉴权彻底失效时应标记 Invalid: %+v", sess)
	}
	if sess.UpdatedAt != 1 {
		t.Fatalf("失效标记不应改动 updated_at（最后一次成功续期时间）: %d", sess.UpdatedAt)
	}
	st := s.TyStatus()
	if st["logged_in"] != false {
		t.Fatalf("失效会话应显示未登录: %v", st)
	}
	if msg, _ := st["status"].(string); !strings.Contains(msg, "重新扫码") {
		t.Fatalf("状态文案应提示重新扫码: %q", msg)
	}
}

func TestTyKeepAliveClearsInvalidOnSuccess(t *testing.T) {
	s := newTyTestServices(t)
	if err := s.saveTySession(&tySession{
		SessionKey: "k", SessionSecret: "s", AccessToken: "a", Invalid: true, UpdatedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"res_code":0}`))
	}))
	defer srv.Close()
	withTyBases(t, srv.URL, srv.URL)

	s.tyKeepAliveOnce(context.Background())

	sess, err := s.loadTySession()
	if err != nil || sess == nil {
		t.Fatalf("load session: %v", err)
	}
	if sess.Invalid {
		t.Fatalf("保活成功后应清除 Invalid: %+v", sess)
	}
	if st := s.TyStatus(); st["logged_in"] != true {
		t.Fatalf("会话恢复后应显示已登录: %v", st)
	}
}

func TestTyIsAuthError(t *testing.T) {
	authErrs := []string{
		"accessToken 已失效(UserInvalidOpenToken)",
		"SessionKey 已失效",
		"refreshToken 换取失败(result=-117): 刷新失败",
		"刷新会话失败(res_code=-1): userSessionBO is null",
	}
	for _, m := range authErrs {
		if !tyIsAuthError(errors.New(m)) {
			t.Fatalf("应判定为鉴权错误: %s", m)
		}
	}
	for _, m := range []string{"dial tcp: i/o timeout", "响应无法解析"} {
		if tyIsAuthError(errors.New(m)) {
			t.Fatalf("不应判定为鉴权错误: %s", m)
		}
	}
}
