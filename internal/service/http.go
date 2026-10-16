package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// sharedHTTPClient 共享 HTTP 客户端（连接复用，低内存设备控制空闲连接数）。
var sharedHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     60 * time.Second,
		DisableKeepAlives:   false,
	},
}

// httpGetText GET 请求并返回响应体文本。
func httpGetText(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Tgto/"+appVersion+" (NAS lightweight build)")
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return string(body), nil
}

// httpPostForm POST 表单并返回响应体文本。
func httpPostForm(ctx context.Context, endpoint string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, stringsReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Tgto/"+appVersion+" (NAS lightweight build)")
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// httpPostJSON POST JSON 并返回响应体文本。
func httpPostJSON(ctx context.Context, endpoint, payload string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, stringsReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Tgto/"+appVersion+" (NAS lightweight build)")
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return string(body), nil
}

// httpPostJSONWithHeaders POST JSON 并附带自定义 Header，返回响应体文本。
func httpPostJSONWithHeaders(ctx context.Context, endpoint, payload string, headers map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, stringsReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Tgto/"+appVersion+" (NAS lightweight build)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return string(body), fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return string(body), nil
}

// httpGetViaProxy 经指定代理发起 GET（用于代理延迟测试）。
func httpGetViaProxy(ctx context.Context, proxyURL, target string) (string, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return "", fmt.Errorf("代理地址解析失败: %w", err)
	}
	client := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(parsed),
			MaxIdleConns:        2,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     20 * time.Second,
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Tgto/"+appVersion+" (NAS lightweight build)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return string(body), nil
}

// stringsReader 字符串转 io.Reader。
func stringsReader(s string) io.Reader {
	return &stringReader{s: s}
}

type stringReader struct {
	s string
	i int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}
