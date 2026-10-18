package service

import "testing"

// 分享链接识别测试：移动云盘（139）+ 天翼，且 #/w/i/ 路由不能被当成提取码。
// 123 云盘已下线，不应再被识别。
func TestParseShareLinkMobile139(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		drive   string
		shareID string
		code    string
	}{
		{
			name:    "分享页路由",
			in:      "https://yun.139.com/shareweb/#/w/i/2qieM8ZkLpQ",
			drive:   Drive139,
			shareID: "2qieM8ZkLpQ",
			code:    "",
		},
		{
			name:    "分享页路由 + 提取码",
			in:      "https://yun.139.com/shareweb/#/w/i/2qieM8ZkLpQ 提取码 k5tt",
			drive:   Drive139,
			shareID: "2qieM8ZkLpQ",
			code:    "k5tt",
		},
		{
			name:    "linkID 查询参数",
			in:      "https://caiyun.139.com/m/i?linkID=abcd1234",
			drive:   Drive139,
			shareID: "abcd1234",
			code:    "",
		},
		{
			name:    "短链 /w/i/",
			in:      "https://yun.139.com/w/i/Zz9Kk8Mm",
			drive:   Drive139,
			shareID: "Zz9Kk8Mm",
			code:    "",
		},
		{
			name:    "链接后跟 #提取码 写法",
			in:      "https://yun.139.com/w/i/Zz9Kk8Mm #zz88",
			drive:   Drive139,
			shareID: "Zz9Kk8Mm",
			code:    "zz88",
		},
		{
			name:    "天翼分享",
			in:      "https://cloud.189.cn/t/eyR3mN7A 6f2q",
			drive:   DriveTianYi,
			shareID: "eyR3mN7A",
			code:    "6f2q",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			link, err := ParseShareLink(c.in)
			if err != nil {
				t.Fatalf("ParseShareLink(%q) error: %v", c.in, err)
			}
			if link.Drive != c.drive {
				t.Fatalf("drive = %q, want %q", link.Drive, c.drive)
			}
			if link.ShareID != c.shareID {
				t.Fatalf("shareID = %q, want %q", link.ShareID, c.shareID)
			}
			if link.Code != c.code {
				t.Fatalf("code = %q, want %q", link.Code, c.code)
			}
		})
	}
}

func TestParseShareLinkUnsupported(t *testing.T) {
	for _, in := range []string{
		"https://example.com/s/abc123",
		"https://123pan.com/s/a1B2c3D4 8x7k", // 123 云盘已下线
	} {
		if _, err := ParseShareLink(in); err == nil {
			t.Fatalf("%s 应报错（不支持的网盘）", in)
		}
	}
}

func TestDriveDisplayNameMobile139(t *testing.T) {
	if got := driveDisplayName(Drive139); got != "移动云盘" {
		t.Fatalf("driveDisplayName(cloud139) = %q, want 移动云盘", got)
	}
}
