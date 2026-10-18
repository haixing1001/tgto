package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestYun139TransferFlow 分享目录查询 → 建目录 → 建转存任务 → 轮询成功。
func TestYun139TransferFlow(t *testing.T) {
	s := newY139TestServices(t)
	if _, err := s.SaveConfig(Drive139, map[string]any{"username": "13771882702"}); err != nil {
		t.Fatal(err)
	}
	if err := s.saveY139Auth(&y139Auth{
		Authorization: "cGM6MTM3NzE4ODI3MDI6ZmFrZS10b2tlbg==",
		Account:       "13771882702",
		ExpiresAt:     time.Now().Add(24 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}

	var savePayload map[string]any
	var createdFolders []string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 路由发现：个人云入口指向测试服务器
	mux.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"` + srv.URL + `"}]}}`))
	})
	// 个人云目录列表：根目录与新建目录都返回空
	mux.HandleFunc("/file/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"items":[]}}`))
	})
	// 建目录：按名称返回递增 ID
	mux.HandleFunc("/file/create", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var in struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &in)
		createdFolders = append(createdFolders, in.Name)
		fid := "F" + itoa64(int64(len(createdFolders)))
		_, _ = w.Write([]byte(`{"success":true,"data":{"fileId":"` + fid + `"}}`))
	})
	// 分享目录
	mux.HandleFunc("/yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"linkID":"SHARE1"`) {
			t.Errorf("分享查询缺少 linkID: %s", string(raw))
		}
		_, _ = w.Write([]byte(`{"code":"0","success":true,"data":{
			"caLst":[{"catalogID":"C1","catalogName":"剧集","path":"/剧集"}],
			"coLst":[{"contentID":"X1","contentName":"a.mkv","path":"/a.mkv","coSize":123}]}}`))
	})
	// 建转存任务
	mux.HandleFunc("/yun-share/richlifeApp/devapp/IBatchOprTask/createOuterLinkBatchOprTask", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &savePayload); err != nil {
			t.Fatalf("转存请求体非法: %v", err)
		}
		_, _ = w.Write([]byte(`{"code":"0","success":true,"data":{"taskID":"T1"}}`))
	})
	// 任务轮询
	mux.HandleFunc("/task/get", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"status":"success"}}`))
	})

	oldRoute, oldShare := y139RouteBase, y139ShareBase
	y139RouteBase, y139ShareBase = srv.URL+"/route", srv.URL
	t.Cleanup(func() { y139RouteBase, y139ShareBase = oldRoute, oldShare })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msg, err := s.Yun139Transfer(ctx, "SHARE1", "abcd", "/媒体库/电影")
	if err != nil {
		t.Fatalf("转存失败: %v", err)
	}
	if !strings.Contains(msg, "1 个文件") || !strings.Contains(msg, "1 个目录") {
		t.Fatalf("转存结果描述异常: %s", msg)
	}
	if strings.Join(createdFolders, ",") != "媒体库,电影" {
		t.Fatalf("应逐级创建 媒体库/电影，实际 %v", createdFolders)
	}

	req, _ := savePayload["createOuterLinkBatchOprTaskReq"].(map[string]any)
	if req == nil {
		t.Fatalf("请求体结构异常: %v", savePayload)
	}
	info, _ := req["taskInfo"].(map[string]any)
	if info == nil {
		t.Fatalf("缺少 taskInfo: %v", req)
	}
	if info["newCatalogID"] != "F2" {
		t.Fatalf("目标目录应为最后创建的 F2，实际 %v", info["newCatalogID"])
	}
	co, _ := info["contentInfoList"].([]any)
	ca, _ := info["catalogInfoList"].([]any)
	if len(co) != 1 || co[0] != "/a.mkv" {
		t.Fatalf("文件列表应为分享内路径 /a.mkv，实际 %v", co)
	}
	if len(ca) != 1 || ca[0] != "/剧集" {
		t.Fatalf("目录列表应为分享内路径 /剧集，实际 %v", ca)
	}
	if req["needPassword"] != true {
		t.Fatalf("有提取码时 needPassword 应为 true: %v", req["needPassword"])
	}
	if req["msisdn"] != "13771882702" {
		t.Fatalf("msisdn 应取配置账号，实际 %v", req["msisdn"])
	}
}

// TestYun139TransferRequiresLogin 未登录时不能执行真实转存。
func TestYun139TransferRequiresLogin(t *testing.T) {
	s := newY139TestServices(t)
	if _, err := s.Yun139Transfer(context.Background(), "SHARE1", "", "/媒体库/电影"); err == nil {
		t.Fatal("未登录时应报错")
	}
}

// TestY139EnsureCatalogRoot 目标为根目录时不再调用建目录接口。
func TestY139EnsureCatalogRoot(t *testing.T) {
	s := newY139TestServices(t)
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"success":true,"data":{"items":[]}}`))
	}))
	defer srv.Close()
	for _, p := range []string{"", "/", "  "} {
		id, err := s.y139EnsureCatalog(context.Background(), "auth", srv.URL, p)
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		if id != "/" {
			t.Fatalf("%q 应返回根目录，实际 %q", p, id)
		}
	}
	if called {
		t.Fatal("根目录不应发起接口请求")
	}
}

// TestY139WaitTaskStatusShapes 真实 /task/get 把状态放在 data.taskInfo.status（"Succeed"），
// 且逐条结果在 batchFileResults[].errCode，两种形态都要能正确判定。
func TestY139WaitTaskStatusShapes(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"taskInfo Succeed", `{"success":true,"code":"0000","data":{"taskInfo":{"status":"Succeed","progress":100},"batchFileResults":[{"errCode":"0000","message":"请求成功"}]}}`, false},
		{"taskInfo Failed", `{"success":true,"code":"0000","data":{"taskInfo":{"status":"Failed","message":"空间不足"}}}`, true},
		{"batch partial fail", `{"success":true,"code":"0000","data":{"taskInfo":{"status":"Succeed"},"batchFileResults":[{"errCode":"200000123","message":"文件已失效"}]}}`, true},
		{"flat status", `{"success":true,"data":{"status":"success"}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newY139TestServices(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			err := s.y139WaitTask(context.Background(), "auth", srv.URL, "T1")
			if c.wantErr && err == nil {
				t.Fatalf("应报错，实际成功")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if c.name == "batch partial fail" && !strings.Contains(err.Error(), "文件已失效") {
				t.Fatalf("错误信息应包含失败原因: %v", err)
			}
		})
	}
}

// TestY139ListFilesParentID 根目录必须传 "/"，空串会被服务端拒绝
// （用户实机报「文件列表请求失败: 父目录ID不允许为空」）。
func TestY139ListFilesParentID(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var in struct {
			ParentFileID string `json:"parentFileId"`
		}
		_ = json.Unmarshal(raw, &in)
		got = in.ParentFileID
		_, _ = w.Write([]byte(`{"success":true,"data":{"items":[]}}`))
	}))
	defer srv.Close()
	s := newY139TestServices(t)
	for _, in := range []string{"", "root", "0", "/", "  "} {
		if _, err := s.y139ListFiles(context.Background(), "auth", srv.URL, in); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != "/" {
			t.Fatalf("输入 %q 应归一化为根目录 /，实际发送 %q", in, got)
		}
	}
}
