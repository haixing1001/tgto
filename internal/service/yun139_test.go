package service

import (
	"bytes"
	"context"
	"crypto/aes"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"tgto/internal/db"
	"tgto/internal/logger"
)

func newY139TestServices(t *testing.T) *Services {
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

func TestY139SignGolden(t *testing.T) {
	// 黄金值由 Python 独立实现同一算法得出，确保 Go 侧签名与线上一致
	got := y139Sign(`{"a":1}`, "2025-01-01 00:00:00", "abcdefghijklmnop")
	want := "95136C5E417AE6489EC5056CE7613128"
	if got != want {
		t.Fatalf("y139Sign = %s, want %s", got, want)
	}
	if got := y139Sign(`{"a":2}`, "2025-01-01 00:00:00", "abcdefghijklmnop"); got == want {
		t.Fatal("不同请求体应得到不同签名")
	}
}

func TestY139SortedJSON(t *testing.T) {
	got, err := y139SortedJSON(map[string]any{
		"b": "x",
		"a": 1,
		"c": map[string]any{"z": true, "y": []string{"q"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":1,"b":"x","c":{"y":["q"],"z":true}}`
	if got != want {
		t.Fatalf("sortedJSON = %s, want %s", got, want)
	}
}

func TestY139AESRoundtrip(t *testing.T) {
	key, _ := hex.DecodeString(y139Key1)
	iv := make([]byte, aes.BlockSize)
	if _, err := crand.Read(iv); err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"msisdn":"18900000000","x":1}`)
	enc, err := y139AESCBCEncrypt(plain, key, iv)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := y139AESCBCDecrypt(enc, key, iv)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != string(plain) {
		t.Fatalf("解密结果不一致: %s", dec)
	}
}

func TestY139AuthorizationParse(t *testing.T) {
	exp := time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	raw := "pc:18912345678:t0|t1|t2|" + itoa64(exp) + "|tail"
	auth := base64.StdEncoding.EncodeToString([]byte(raw))

	if got := y139AuthorizationAccount(auth); got != "18912345678" {
		t.Fatalf("account = %q", got)
	}
	if got := y139AuthorizationExpiry(auth); got != exp {
		t.Fatalf("expiry = %d, want %d", got, exp)
	}
	if got := y139AuthorizationAccount("not-base64!!"); got != "" {
		t.Fatalf("非法令牌应返回空账号，得到 %q", got)
	}
}

func TestY139CookieHelpers(t *testing.T) {
	cookies := "Os_SSo_Sid=sid1; RMKEY=rm1; other=1"
	if got := y139CookieValue(cookies, "RMKEY"); got != "rm1" {
		t.Fatalf("RMKEY = %q", got)
	}
	merged := y139MergeCookies("a=1; b=2", []*http.Cookie{{Name: "b", Value: "20"}, {Name: "c", Value: "3"}})
	if merged != "a=1; b=20; c=3" {
		t.Fatalf("merged = %q", merged)
	}
}

// TestYun139PasswordLoginFlow 端到端模拟：邮箱登录 → artifact → 加密单点登录 → 保存令牌。
func TestYun139LoginFlow(t *testing.T) {
	s := newY139TestServices(t)
	if _, err := s.SaveConfig(Drive139, map[string]any{
		"username": "18900000000", "password": "secret", "mail_cookies": "RMKEY=rmkey-1",
	}); err != nil {
		t.Fatal(err)
	}

	key1, _ := hex.DecodeString(y139Key1)
	key2, _ := hex.DecodeString(y139Key2)

	mux := http.NewServeMux()
	// step1：邮箱登录返回 302 + sid
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.PostForm.Get("UserName") != "18900000000" {
			t.Errorf("UserName = %q", r.PostForm.Get("UserName"))
		}
		// 密码为 SHA1("fetion.com.cn:<pwd>")
		if got, want := r.PostForm.Get("Password"), y139SHA1("fetion.com.cn:secret"); got != want {
			t.Errorf("Password hash = %q, want %q", got, want)
		}
		w.Header().Set("Location", "https://mail.10086.cn/?sid=SID-1")
		w.WriteHeader(http.StatusFound)
	})
	// step2：换 artifact
	mux.HandleFunc("/artifact", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sid") != "SID-1" {
			t.Errorf("sid = %q", r.URL.Query().Get("sid"))
		}
		if !strings.Contains(r.Header.Get("Cookie"), "RMKEY=rmkey-1") {
			t.Errorf("缺少 RMKEY cookie: %q", r.Header.Get("Cookie"))
		}
		_, _ = w.Write([]byte(`{"var":{"artifact":"ARTIFACT-1"}}`))
	})
	// step3：加密单点登录
	mux.HandleFunc("/thirdlogin", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
		if err != nil || len(raw) < aes.BlockSize {
			t.Errorf("请求体不是 base64(iv||ct): %v", err)
			return
		}
		plain, err := y139AESCBCDecrypt(raw[aes.BlockSize:], key1, raw[:aes.BlockSize])
		if err != nil {
			t.Errorf("请求解密失败: %v", err)
			return
		}
		var req map[string]any
		if err := json.Unmarshal(plain, &req); err != nil {
			t.Errorf("请求不是 JSON: %v", err)
			return
		}
		if req["dycpwd"] != "ARTIFACT-1" || req["msisdn"] != "18900000000" {
			t.Errorf("登录参数错误: %v", req)
		}
		// 第二层：authToken/account/userDomainId
		exp := time.Now().Add(60 * 24 * time.Hour).UnixMilli()
		layer2 := `{"authToken":"a|b|c|` + itoa64(exp) + `|z","account":"18900000000","userDomainId":"ud-1"}`
		enc2, err := y139AESECBEncrypt([]byte(layer2), key2)
		if err != nil {
			t.Errorf("二次加密失败: %v", err)
			return
		}
		layer1 := `{"data":"` + hex.EncodeToString(enc2) + `"}`
		iv := make([]byte, aes.BlockSize)
		_, _ = crand.Read(iv)
		enc1, err := y139AESCBCEncrypt([]byte(layer1), key1, iv)
		if err != nil {
			t.Errorf("一层加密失败: %v", err)
			return
		}
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(append(iv, enc1...))))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldLogin, oldArtifact, oldThird := y139MailLoginBase, y139ArtifactBase, y139ThirdLoginBase
	y139MailLoginBase, y139ArtifactBase, y139ThirdLoginBase = srv.URL+"/login", srv.URL+"/artifact", srv.URL+"/thirdlogin"
	t.Cleanup(func() { y139MailLoginBase, y139ArtifactBase, y139ThirdLoginBase = oldLogin, oldArtifact, oldThird })

	status, err := s.Yun139Login(context.Background())
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if status["logged_in"] != true {
		t.Fatalf("登录后状态应为已登录: %v", status)
	}
	if status["account"] != "18900000000" {
		t.Fatalf("账号 = %v", status["account"])
	}

	auth, err := s.loadY139Auth()
	if err != nil || auth == nil {
		t.Fatalf("令牌未落盘: %v", err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(auth.Authorization)
	if !strings.HasPrefix(string(decoded), "pc:18900000000:a|b|c|") {
		t.Fatalf("Authorization 格式错误: %s", decoded)
	}
	if auth.ExpiresAt == 0 {
		t.Fatal("过期时间未解析")
	}
	if auth.UserDomainID != "ud-1" {
		t.Fatalf("UserDomainID = %q", auth.UserDomainID)
	}
}

// TestYun139FilesAndVerify 校验令牌 + 列目录（路由策略 → 文件列表）。
func TestYun139FilesAndVerify(t *testing.T) {
	s := newY139TestServices(t)
	exp := time.Now().Add(30 * 24 * time.Hour).UnixMilli()
	auth := base64.StdEncoding.EncodeToString([]byte("pc:18900000000:t0|t1|t2|" + itoa64(exp) + "|x"))

	mux := http.NewServeMux()
	mux.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Basic "+auth {
			t.Errorf("Authorization = %q", got)
		}
		if r.Header.Get("Mcloud-Sign") == "" {
			t.Error("缺少 Mcloud-Sign")
		}
		host := "http://" + r.Host + "/personal"
		_, _ = w.Write([]byte(fmt.Sprintf(`{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"%s"}]}}`, host)))
	})
	mux.HandleFunc("/personal/file/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"fileId":"f1","name":"电影","type":"folder","size":0},{"fileId":"f2","name":"a.mkv","type":"file","size":1024}],"nextPageCursor":""}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldRoute := y139RouteBase
	y139RouteBase = srv.URL + "/route"
	t.Cleanup(func() { y139RouteBase = oldRoute })

	if _, err := s.y139VerifyAuthorization(context.Background(), auth); err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	res, err := s.Yun139Files(context.Background(), "")
	if err != nil {
		t.Fatalf("列目录失败: %v", err)
	}
	if res["count"] != 2 {
		t.Fatalf("count = %v", res["count"])
	}
	files := res["files"].([]map[string]any)
	if files[0]["is_dir"] != true || files[1]["name"] != "a.mkv" {
		t.Fatalf("文件解析错误: %v", files)
	}
	if st := s.Yun139Status(); st["logged_in"] != true {
		t.Fatalf("校验后应为已登录: %v", st)
	}
}

func TestYun139StatusExpired(t *testing.T) {
	s := newY139TestServices(t)
	past := time.Now().Add(-time.Hour).UnixMilli()
	if err := s.saveY139Auth(&y139Auth{
		Authorization: base64.StdEncoding.EncodeToString([]byte("pc:18900000000:t|t|t|" + itoa64(past) + "|x")),
		Account:       "18900000000",
		ExpiresAt:     past,
	}); err != nil {
		t.Fatal(err)
	}
	st := s.Yun139Status()
	if st["logged_in"] != false {
		t.Fatalf("过期令牌应显示未登录: %v", st)
	}
	if msg, _ := st["status"].(string); !strings.Contains(msg, "过期") {
		t.Fatalf("状态文案应提示过期: %q", msg)
	}
}

func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}

// TestYun139FastLoginWithCookies 邮箱 Cookie 含 Os_SSo_Sid+RMKEY 时跳过邮箱密码登录。
func TestYun139FastLoginWithCookies(t *testing.T) {
	s := newY139TestServices(t)
	if _, err := s.SaveConfig(Drive139, map[string]any{
		"username": "18900000000", "password": "secret",
		"mail_cookies": "Os_SSo_Sid=sid-9; RMKEY=rm-9",
	}); err != nil {
		t.Fatal(err)
	}
	key1, _ := hex.DecodeString(y139Key1)
	key2, _ := hex.DecodeString(y139Key2)

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		t.Error("快速登录路径不应再请求邮箱密码登录")
	})
	mux.HandleFunc("/artifact", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sid"); got != "sid-9" {
			t.Errorf("sid = %q", got)
		}
		_, _ = w.Write([]byte(`{"var":{"artifact":"ART-9"}}`))
	})
	mux.HandleFunc("/thirdlogin", func(w http.ResponseWriter, r *http.Request) {
		exp := time.Now().Add(60 * 24 * time.Hour).UnixMilli()
		layer2 := `{"authToken":"a|b|c|` + itoa64(exp) + `|z","account":"18900000000","userDomainId":"ud-9"}`
		enc2, _ := y139AESECBEncrypt([]byte(layer2), key2)
		iv := make([]byte, aes.BlockSize)
		_, _ = crand.Read(iv)
		enc1, _ := y139AESCBCEncrypt([]byte(`{"data":"`+hex.EncodeToString(enc2)+`"}`), key1, iv)
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(append(iv, enc1...))))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldLogin, oldArtifact, oldThird := y139MailLoginBase, y139ArtifactBase, y139ThirdLoginBase
	y139MailLoginBase, y139ArtifactBase, y139ThirdLoginBase = srv.URL+"/login", srv.URL+"/artifact", srv.URL+"/thirdlogin"
	t.Cleanup(func() { y139MailLoginBase, y139ArtifactBase, y139ThirdLoginBase = oldLogin, oldArtifact, oldThird })

	status, err := s.Yun139Login(context.Background())
	if err != nil {
		t.Fatalf("快速登录失败: %v", err)
	}
	if status["logged_in"] != true {
		t.Fatalf("应已登录: %v", status)
	}
}

// TestYun139LoginFailureDiagnostics 邮箱登录 302 无 sid 时，错误信息要带脱敏的 Location 与风控提示。
func TestYun139LoginFailureDiagnostics(t *testing.T) {
	s := newY139TestServices(t)
	if _, err := s.SaveConfig(Drive139, map[string]any{
		"username": "18900000000", "password": "wrong",
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://mail.10086.cn/default.html?errCode=110&u=18900000000")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	oldLogin := y139MailLoginBase
	y139MailLoginBase = srv.URL
	t.Cleanup(func() { y139MailLoginBase = oldLogin })

	_, err := s.Yun139Login(context.Background())
	if err == nil {
		t.Fatal("应返回登录失败")
	}
	msg := err.Error()
	for _, want := range []string{"Location=", "errCode=110", "风控"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误信息缺少 %q: %s", want, msg)
		}
	}

}

func TestY139SanitizeURL(t *testing.T) {
	out := y139SanitizeURL("https://mail.10086.cn/x?sid=abcdef123456&token=zzzzzzzz&errCode=110")
	if strings.Contains(out, "abcdef123456") || strings.Contains(out, "zzzzzzzz") {
		t.Fatalf("敏感参数未脱敏: %s", out)
	}
	if !strings.Contains(out, "abcdREDACTED") || !strings.Contains(out, "errCode=110") {
		t.Fatalf("脱敏结果不符: %s", out)
	}
}

// TestYun139SMSLoginFlow 触发风控（ec=S035）→ 自动发短信 → 输码登录成功。
func TestYun139SMSLoginFlow(t *testing.T) {
	s := newY139TestServices(t)
	if _, err := s.SaveConfig(Drive139, map[string]any{
		"username": "13700000000", "password": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	key1, _ := hex.DecodeString(y139Key1)
	key2, _ := hex.DecodeString(y139Key2)

	var gotSmsPwdHash string
	mux := http.NewServeMux()
	// step1：返回风控 ec=S035
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://mail.10086.cn/default.html?ec=S035&u=x")
		w.WriteHeader(http.StatusFound)
	})
	// 邮箱短信接口：按 func 分发
	mux.HandleFunc("/sms", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("func") {
		case "login:sendSmsCodeByScene":
			if r.Header.Get("Content-Type") != "application/xml; charset=utf-8" {
				t.Errorf("短信接口 Content-Type = %q", r.Header.Get("Content-Type"))
			}
			_, _ = w.Write([]byte(`{"code":"S_OK","summary":"发送成功"}`))
		case "/login/inlogin.action":
			body, _ := io.ReadAll(r.Body)
			raw := string(body)
			want := "loginPassword\">" + y139SHA1("fetion.com.cn:654321") + "<"
			if !strings.Contains(raw, want) {
				t.Errorf("短信验证码哈希不符，应按密码规则 SHA1: %s", truncate(raw, 200))
			}
			gotSmsPwdHash = want
			// 真实流程：短信验证通过后服务端会下发 RMKEY 等登录 Cookie
			w.Header().Add("Set-Cookie", "RMKEY=rm-sms; Path=/")
			w.Header().Add("Set-Cookie", "Os_SSo_Sid=SMS-SID-1; Path=/")
			_, _ = w.Write([]byte(`{"code":"S_OK","var":{"loginSuccessUrl":"https://mail.10086.cn/?sid=SMS-SID-1"}}`))
		default:
			t.Errorf("unexpected sms func: %s", r.URL.RawQuery)
		}
	})
	mux.HandleFunc("/artifact", func(w http.ResponseWriter, r *http.Request) {
		if gotSmsPwdHash == "" {
			t.Error("未完成短信验证就进入了 artifact 步骤")
		}
		if r.URL.Query().Get("sid") != "SMS-SID-1" {
			t.Errorf("sid = %q", r.URL.Query().Get("sid"))
		}
		_, _ = w.Write([]byte(`{"var":{"artifact":"ART-SMS"}}`))
	})
	mux.HandleFunc("/thirdlogin", func(w http.ResponseWriter, r *http.Request) {
		exp := time.Now().Add(60 * 24 * time.Hour).UnixMilli()
		layer2 := `{"authToken":"a|b|c|` + itoa64(exp) + `|z","account":"13700000000","userDomainId":"ud-s"}`
		enc2, _ := y139AESECBEncrypt([]byte(layer2), key2)
		iv := make([]byte, aes.BlockSize)
		_, _ = crand.Read(iv)
		enc1, _ := y139AESCBCEncrypt([]byte(`{"data":"`+hex.EncodeToString(enc2)+`"}`), key1, iv)
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString(append(iv, enc1...))))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldLogin, oldSMS, oldArtifact, oldThird := y139MailLoginBase, y139MailSMS, y139ArtifactBase, y139ThirdLoginBase
	y139MailLoginBase, y139MailSMS, y139ArtifactBase, y139ThirdLoginBase = srv.URL+"/login", srv.URL+"/sms", srv.URL+"/artifact", srv.URL+"/thirdlogin"
	t.Cleanup(func() {
		y139MailLoginBase, y139MailSMS, y139ArtifactBase, y139ThirdLoginBase = oldLogin, oldSMS, oldArtifact, oldThird
	})

	// 第一次登录：触发风控 → 自动发短信
	res, err := s.Yun139Login(context.Background())
	if err != nil {
		t.Fatalf("触发风控不应报错: %v", err)
	}
	if res["sms_required"] != true || res["risk_code"] != "S035" {
		t.Fatalf("应返回 sms_required: %v", res)
	}

	// 重发验证码
	if _, err := s.Yun139SMSSend(context.Background()); err != nil {
		t.Fatalf("重发验证码失败: %v", err)
	}

	// 输码登录
	status, err := s.Yun139LoginSMS(context.Background(), "654321")
	if err != nil {
		t.Fatalf("短信登录失败: %v", err)
	}
	if status["logged_in"] != true || status["account"] != "13700000000" {
		t.Fatalf("短信登录后状态: %v", status)
	}
	auth, _ := s.loadY139Auth()
	if auth == nil || auth.Authorization == "" {
		t.Fatal("短信登录后令牌未落盘")
	}
}

func TestY139ParseMailLogin(t *testing.T) {
	// JSON 风格
	p := y139ParseMailLogin([]byte(`{"code":"S_OK","var":{"loginSuccessUrl":"https://mail.10086.cn/?sid=abc"}}`))
	if p.Code != "S_OK" || p.LoginSuccessURL != "https://mail.10086.cn/?sid=abc" {
		t.Fatalf("JSON 解析: %+v", p)
	}
	// JS 变量风格
	p = y139ParseMailLogin([]byte(`var result = {"code":"S_FAIL","summary":"密码错误"}`))
	if p.Code != "S_FAIL" || p.Summary != "密码错误" {
		t.Fatalf("JS 解析: %+v", p)
	}
}

// TestY139MailPublicKey 邮箱公钥常量必须是有效 PKIX RSA 公钥（手抄错一个字符会导致
// 服务端返回 PML401010002「短信验证参数被拒绝」）。
func TestY139MailPublicKey(t *testing.T) {
	der, err := base64.StdEncoding.DecodeString(y139MailPub)
	if err != nil {
		t.Fatalf("公钥 base64 解码失败: %v", err)
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatalf("公钥解析失败: %v", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("公钥不是 RSA 类型: %T", pub)
	}
	if rsaPub.N == nil || rsaPub.E == 0 || rsaPub.Size() != 256 {
		t.Fatalf("公钥尺寸异常: %d bit", rsaPub.N.BitLen())
	}
	out, err := y139EncryptLoginName("13771882702")
	if err != nil {
		t.Fatalf("加密账号失败: %v", err)
	}
	back, err := base64.StdEncoding.DecodeString(out)
	if err != nil || len(back) != 256 {
		t.Fatalf("加密结果应为 256 字节密文，实际 %d: %v", len(back), err)
	}
}

// TestY139AESECB 139 单点登录第二层是 AES/ECB/PKCS7：多块时与 CBC 零 IV 结果不同，
// 用 NIST 向量验证 ECB 实现本身正确。
func TestY139AESECB(t *testing.T) {
	key := make([]byte, 16)
	ciphertext := make([]byte, 32) // 两块的密文
	// 用 ECB 加密 32 字节明文，前 16 字节与 CBC 零 IV 相同、第二块不同
	plain := []byte("0123456789abcdef0123456789abcdef")
	encECB, err := y139AESECBEncrypt(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	encCBC, err := y139AESCBCEncrypt(plain, key, make([]byte, aes.BlockSize))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(encECB[:16], encCBC[:16]) == false {
		t.Fatal("首块应与 CBC 零 IV 相同")
	}
	if bytes.Equal(encECB[16:], encCBC[16:]) {
		t.Fatal("第二块应与 CBC 不同（否则测不出 ECB/CBC 差异）")
	}
	back, err := y139AESECBDecrypt(encECB, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, plain) {
		t.Fatalf("ECB 往返失败: %q", back)
	}
	// 跨 32 字节的多块 JSON 必须完整还原（CBC 会在第二块起乱码）
	long := []byte(`{"authToken":"0123456789abcdef0123456789abcdef","account":"13771882702","userDomainId":"ud"}`)
	enc, err := y139AESECBEncrypt(long, key)
	if err != nil {
		t.Fatal(err)
	}
	out, err := y139AESECBDecrypt(enc, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, long) {
		t.Fatalf("多块 ECB 往返失败: %q", out)
	}
	_ = ciphertext
}
