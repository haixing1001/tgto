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

// y139FilesTestServer 造一个覆盖 139 个人云文件接口的假服务端，返回请求体记录。
func y139FilesTestServer(t *testing.T, seen map[string]map[string]any) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	srv = httptest.NewServer(mux)
	mux.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"` + srv.URL + `"}]}}`))
	})
	record := func(name string, body []byte) {
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		seen[name] = m
	}
	mux.HandleFunc("/file/list", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("list", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"items":[
			{"fileId":"D1","name":"电影","type":"folder","size":0,"updatedAt":"2026-10-18T10:00:00+08:00"},
			{"fileId":"F1","name":"a.mkv","type":"file","size":2048,"updatedAt":"2026-10-18T11:00:00+08:00"}],
			"nextPageCursor":""}}`))
	})
	mux.HandleFunc("/file/create", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("create", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"fileId":"NEW1"}}`))
	})
	mux.HandleFunc("/file/update", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("update", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{}}`))
	})
	mux.HandleFunc("/file/batchMove", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("move", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"taskId":"TM"}}`))
	})
	mux.HandleFunc("/file/batchCopy", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("copy", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"taskId":"TC"}}`))
	})
	mux.HandleFunc("/recyclebin/batchTrash", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("trash", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"taskId":"TT"}}`))
	})
	mux.HandleFunc("/recyclebin/clear", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("clear", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"taskId":"TCL"}}`))
	})
	mux.HandleFunc("/file/getDownloadUrl", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		record("download", raw)
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"url":"https://cdn.example.com/a.mkv?sig=1"}}`))
	})
	mux.HandleFunc("/task/get", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"code":"0000","data":{"taskInfo":{"status":"Succeed","progress":100}}}`))
	})
	return srv
}

func newY139FilesServices(t *testing.T, seen map[string]map[string]any) (*Services, func()) {
	t.Helper()
	s := newY139TestServices(t)
	if _, err := s.SaveConfig(Drive139, map[string]any{"username": "13771882702"}); err != nil {
		t.Fatal(err)
	}
	if err := s.saveY139Auth(&y139Auth{
		Authorization: "cGM6MTM3NzE4ODI3MDI6ZmFrZQ==",
		Account:       "13771882702",
		ExpiresAt:     time.Now().Add(24 * time.Hour).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	srv := y139FilesTestServer(t, seen)
	oldRoute := y139RouteBase
	y139RouteBase = srv.URL + "/route"
	return s, func() {
		y139RouteBase = oldRoute
		srv.Close()
	}
}

func TestYun139FileList(t *testing.T) {
	seen := map[string]map[string]any{}
	s, done := newY139FilesServices(t, seen)
	defer done()

	res, err := s.Yun139FileList(context.Background(), "", 200)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if res["count"] != 2 {
		t.Fatalf("应返回 2 项: %v", res)
	}
	if seen["list"]["parentFileId"] != "/" {
		t.Fatalf("根目录应传 /，实际 %v", seen["list"]["parentFileId"])
	}
	items := res["items"].([]map[string]any)
	if items[0]["isDir"] != true || items[1]["isDir"] != false {
		t.Fatalf("目录/文件判定错误: %v", items)
	}
}

func TestYun139FileOps(t *testing.T) {
	seen := map[string]map[string]any{}
	s, done := newY139FilesServices(t, seen)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := s.Yun139Mkdir(ctx, "新目录", "/"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if seen["create"]["name"] != "新目录" || seen["create"]["type"] != "folder" {
		t.Fatalf("mkdir 请求体: %v", seen["create"])
	}

	if _, err := s.Yun139Rename(ctx, "F1", "b.mkv"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if seen["update"]["fileId"] != "F1" || seen["update"]["name"] != "b.mkv" {
		t.Fatalf("rename 请求体: %v", seen["update"])
	}

	if _, err := s.Yun139Move(ctx, []string{"F1", " "}, "D1"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if ids, _ := seen["move"]["fileIds"].([]any); len(ids) != 1 || ids[0] != "F1" {
		t.Fatalf("move 应过滤空 ID: %v", seen["move"])
	}
	if seen["move"]["toParentFileId"] != "D1" {
		t.Fatalf("move 目标目录: %v", seen["move"])
	}

	if _, err := s.Yun139Copy(ctx, []string{"F1"}, "/"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if seen["copy"]["toParentFileId"] != "/" {
		t.Fatalf("copy 根目录应为 /: %v", seen["copy"])
	}

	if _, err := s.Yun139Delete(ctx, []string{"F1"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if seen["trash"]["fileIds"] == nil {
		t.Fatalf("delete 请求体: %v", seen["trash"])
	}

	if _, err := s.Yun139ClearRecycle(ctx); err != nil {
		t.Fatalf("clear recycle: %v", err)
	}

	res, err := s.Yun139DownloadURL(ctx, "F1")
	if err != nil {
		t.Fatalf("download url: %v", err)
	}
	if !strings.Contains(res["url"].(string), "cdn.example.com") {
		t.Fatalf("下载地址异常: %v", res)
	}
}

// TestYun139FileOpsRequireLogin 未登录时所有文件操作都应报错而不是空转。
func TestYun139FileOpsRequireLogin(t *testing.T) {
	s := newY139TestServices(t)
	ctx := context.Background()
	if _, err := s.Yun139FileList(ctx, "/", 10); err == nil {
		t.Fatal("列表应要求登录")
	}
	if _, err := s.Yun139Mkdir(ctx, "x", "/"); err == nil {
		t.Fatal("mkdir 应要求登录")
	}
	if _, err := s.Yun139Delete(ctx, []string{"F1"}); err == nil {
		t.Fatal("delete 应要求登录")
	}
}
