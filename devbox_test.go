package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func TestKongCLIParsing(t *testing.T) {
	type testCLI struct {
		Dir     string           `arg:"" optional:"" default:"." help:"devbox.toml と contents/ を含む作業ディレクトリ"`
		Version kong.VersionFlag `short:"v" help:"バージョンを表示して終了"`
	}
	newParser := func(target *testCLI, out *bytes.Buffer) *kong.Kong {
		t.Helper()
		parser, err := kong.New(target,
			kong.Name("devbox"),
			kong.Vars{"version": "1.1.0"},
			kong.Writers(out, out),
			kong.Exit(func(int) {}),
		)
		if err != nil {
			t.Fatalf("kong.New失敗: %v", err)
		}
		return parser
	}

	// 引数なしは "."
	var c1 testCLI
	var out bytes.Buffer
	if _, err := newParser(&c1, &out).Parse(nil); err != nil {
		t.Fatalf("引数なしのパース失敗: %v", err)
	}
	if c1.Dir != "." {
		t.Errorf("デフォルトDir不正: got %q want \".\"", c1.Dir)
	}

	// 引数1つ
	var c2 testCLI
	if _, err := newParser(&c2, &out).Parse([]string{"./work"}); err != nil {
		t.Fatalf("引数1つのパース失敗: %v", err)
	}
	if c2.Dir != "./work" {
		t.Errorf("Dir不正: got %q want \"./work\"", c2.Dir)
	}

	// 引数2つはエラー
	var c3 testCLI
	if _, err := newParser(&c3, &out).Parse([]string{"a", "b"}); err == nil {
		t.Fatal("引数2つでエラーが返りませんでした")
	}

	// --version / -v でバージョン表示
	for _, flag := range []string{"--version", "-v"} {
		var cVer testCLI
		out.Reset()
		if _, err := newParser(&cVer, &out).Parse([]string{flag}); err != nil {
			t.Fatalf("%s のパース失敗: %v", flag, err)
		}
		if strings.TrimSpace(out.String()) != "1.1.0" {
			t.Errorf("%s の出力不正: got %q want \"1.1.0\"", flag, out.String())
		}
	}
}

func TestLoadConfig(t *testing.T) {
	workDir := t.TempDir()
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		t.Fatal(err)
	}
	wantLog := filepath.Join(absWork, "logs", "devbox.log")
	tomlContent := fmt.Sprintf(`
log_file = %q
log_format = "json"
cloudflare = true

[[users]]
user = "alice"
pass = "secret"
directory = "alice"
`, wantLog)
	tomlPath := filepath.Join(workDir, "devbox.toml")
	if err := os.WriteFile(tomlPath, []byte(tomlContent), 0600); err != nil {
		t.Fatal(err)
	}

	c, err := LoadConfig(workDir)
	if err != nil {
		t.Fatalf("LoadConfig失敗: %v", err)
	}

	if c.BaseDir != absWork {
		t.Errorf("BaseDir mismatch: got %q want %q", c.BaseDir, absWork)
	}
	if c.Addr != ":8080" || c.SessionDays != 14 {
		t.Errorf("デフォルト値不正: addr=%q session_days=%d", c.Addr, c.SessionDays)
	}
	if c.ContentsDir() != filepath.Join(absWork, "contents") {
		t.Errorf("ContentsDir mismatch: got %q", c.ContentsDir())
	}
	if c.LogFile != wantLog {
		t.Errorf("LogFile mismatch: got %q want %q", c.LogFile, wantLog)
	}
	if !c.Cloudflare || c.LogFormat != "json" {
		t.Errorf("設定値反映不正: cloudflare=%v log_format=%q", c.Cloudflare, c.LogFormat)
	}

	// ファイル直指定はエラーにする
	if _, err := LoadConfig(tomlPath); err == nil {
		t.Error("tomlファイルを直接渡したのにエラーになりませんでした")
	}

	// 相対パスの log_file はエラーにする
	relLogDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(relLogDir, "devbox.toml"), []byte("log_file = \"devbox.log\"\n[[users]]\nuser=\"a\"\npass=\"b\"\ndirectory=\"a\"\n"), 0600)
	if _, err := LoadConfig(relLogDir); err == nil {
		t.Error("相対パスのlog_fileでエラーになりませんでした")
	}

	// 不正な log_format
	badFormatDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(badFormatDir, "devbox.toml"), []byte("log_format = \"yaml\"\n[[users]]\nuser=\"a\"\npass=\"b\"\ndirectory=\"a\"\n"), 0600)
	if _, err := LoadConfig(badFormatDir); err == nil {
		t.Error("不正なlog_formatでエラーになりませんでした")
	}

	// 不正な users.directory
	badUserDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(badUserDir, "devbox.toml"), []byte("[[users]]\nuser=\"a\"\npass=\"b\"\ndirectory=\"../etc\"\n"), 0600)
	if _, err := LoadConfig(badUserDir); err == nil {
		t.Error("パストラバーサルを含むdirectoryでエラーになりませんでした")
	}
}

func TestHandlersAndStructuredLogging(t *testing.T) {
	workDir := t.TempDir()
	demoDir := filepath.Join(workDir, "contents", "demo")
	if err := os.MkdirAll(demoDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(demoDir, "index.md"), []byte("# Hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sampleBytes := []byte("hello devbox download content")
	if err := os.WriteFile(filepath.Join(demoDir, "sample.txt"), sampleBytes, 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(demoDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "only_in_sub.txt"), []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	tomlContent := `
log_format = "json"
cloudflare = true

[[users]]
user = "demo"
pass = "demo"
directory = "demo"
`
	if err := os.WriteFile(filepath.Join(workDir, "devbox.toml"), []byte(tomlContent), 0600); err != nil {
		t.Fatal(err)
	}

	var err error
	cfg, err = LoadConfig(workDir)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	sessions = NewSessionStore()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	handler := loggingMiddleware(logger, true, newServeMux())

	decodeLastLog := func() map[string]any {
		t.Helper()
		lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
		var entry map[string]any
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
			t.Fatalf("JSONログのパース失敗: %v (raw=%s)", err, lines[len(lines)-1])
		}
		return entry
	}

	// 1. ログイン失敗
	form := url.Values{"user": {"wronguser"}, "pass": {"bad"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	req.Header.Set("CF-IPCountry", "JP")
	req.Header.Set("CF-Ray", "ray-001")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ログイン失敗時のステータス不正: %d", rec.Code)
	}
	entry := decodeLastLog()
	if entry["msg"] != "ログイン失敗" || entry["level"] != "WARN" || entry["user"] != "wronguser" {
		t.Errorf("ログイン失敗ログ不正: %v", entry)
	}
	if entry["ip"] != "203.0.113.10" || entry["country"] != "JP" || entry["cf_ray"] != "ray-001" {
		t.Errorf("Cloudflareヘッダー記録不正: %v", entry)
	}

	// 2. ログイン成功
	form = url.Values{"user": {"demo"}, "pass": {"demo"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	req.Header.Set("CF-IPCountry", "JP")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("ログイン成功時のステータス不正: %d", rec.Code)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("セッションCookieが発行されませんでした")
	}
	sessionCookie := cookies[0]
	entry = decodeLastLog()
	if entry["msg"] != "ログイン成功" || entry["user"] != "demo" {
		t.Errorf("ログイン成功ログ不正: %v", entry)
	}

	// 3. トップページ表示
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("indexステータス不正: %d", rec.Code)
	}
	entry = decodeLastLog()
	if entry["msg"] != "アクセス" || entry["user"] != "demo" || entry["path"] != "/" {
		t.Errorf("アクセスログ不正: %v", entry)
	}

	// 4. ファイルダウンロード
	req = httptest.NewRequest(http.MethodGet, "/download/sample.txt", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ダウンロードステータス不正: %d", rec.Code)
	}
	entry = decodeLastLog()
	if entry["msg"] != "ダウンロード" || entry["path"] != "/download/sample.txt" {
		t.Errorf("ダウンロードログ不正: %v", entry)
	}
	if _, exists := entry["file"]; exists {
		t.Errorf("不要なfileキーが出力されています: %v", entry)
	}
	if int(entry["file_size"].(float64)) != len(sampleBytes) || int(entry["bytes"].(float64)) != len(sampleBytes) {
		t.Errorf("サイズ計測不正: file_size=%v bytes=%v want=%d", entry["file_size"], entry["bytes"], len(sampleBytes))
	}

	// 5. /download/ 単体やサブディレクトリ配下・パストラバーサルは404
	for _, p := range []string{"/download/", "/download/sub/only_in_sub.txt", "/download/..%2Fdevbox.toml"} {
		req = httptest.NewRequest(http.MethodGet, p, nil)
		req.AddCookie(sessionCookie)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s は404になるべきですが %d でした", p, rec.Code)
		}
	}

	// 6. cloudflare=false のときはCFヘッダーを無視し、X-Forwarded-For / RemoteAddrを使う
	logBuf.Reset()
	nonCFHandler := loggingMiddleware(logger, false, newServeMux())
	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	req.RemoteAddr = "192.0.2.50:12345"
	req.Header.Set("CF-Connecting-IP", "198.51.100.99")
	req.Header.Set("CF-IPCountry", "XX")
	rec = httptest.NewRecorder()
	nonCFHandler.ServeHTTP(rec, req)

	entry = decodeLastLog()
	if entry["ip"] != "192.0.2.50" {
		t.Errorf("cloudflare=false時のIP不正: got %v want 192.0.2.50", entry["ip"])
	}
	if _, exists := entry["country"]; exists {
		t.Errorf("cloudflare=false時にcountryが出力されています: %v", entry)
	}
}

func TestNewLoggerFileOutput(t *testing.T) {
	workDir := t.TempDir()
	c := &Config{
		BaseDir:   workDir,
		LogFile:   filepath.Join(workDir, "logs", "devbox.log"),
		LogFormat: "json",
	}
	logger, cleanup, err := newLogger(c)
	if err != nil {
		t.Fatalf("newLogger失敗: %v", err)
	}
	logger.Info("起動", slog.String("addr", ":8080"))
	cleanup()

	raw, err := os.ReadFile(c.LogFile)
	if err != nil {
		t.Fatalf("ログファイル読み込み失敗: %v", err)
	}
	if !strings.Contains(string(raw), `"msg":"起動"`) {
		t.Errorf("ログファイル出力内容が想定外です: %s", string(raw))
	}
}
