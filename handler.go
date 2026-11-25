package main

import (
	"bytes"
	"context"
	"html/template"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuin/goldmark"
)

type ctxKey string

const (
	userCtxKey        ctxKey = "user"
	metaCtxKey        ctxKey = "request_meta"
	sessionCookieName string = "session_id"
)

// requestMeta: ハンドラ間で共有し、最終的にloggingMiddlewareが出力するリクエストメタ情報
type requestMeta struct {
	Msg      string
	Level    slog.Level
	User     string
	FileSize int64
	HasFile  bool
}

func getRequestMeta(ctx context.Context) *requestMeta {
	if meta, ok := ctx.Value(metaCtxKey).(*requestMeta); ok {
		return meta
	}
	return nil
}

// responseWriterWrapper: 出力されたHTTPステータスコードと送信バイト数をキャプチャするためのラッパー
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
	bytes      int64
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += int64(n)
	return n, err
}

// clientIP: X-Forwarded-Forヘッダー（先頭のIP）があればそれを、なければRemoteAddrを返す
// cloudflare有効時はCF-Connecting-IPを最優先する
// cloudflaredなどのプロキシ経由の場合、RemoteAddrはプロキシのアドレスになるため
func clientIP(r *http.Request, cloudflare bool) string {
	if cloudflare {
		if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
			return cfIP
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// loggingMiddleware: すべてのリクエストのアクセスログ（IP、ユーザー名、メソッド、URL、ステータスコード、処理時間）を出力
func loggingMiddleware(logger *slog.Logger, cloudflare bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		meta := &requestMeta{
			Msg:   "アクセス",
			Level: slog.LevelInfo,
			User:  "-",
		}
		ctx := context.WithValue(r.Context(), metaCtxKey, meta)
		wrapped := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r.WithContext(ctx))

		durationMs := math.Round(float64(time.Since(start).Microseconds())/10.0) / 100.0
		ip := clientIP(r, cloudflare)

		attrs := make([]slog.Attr, 0, 12)
		attrs = append(attrs, slog.String("ip", ip))
		if cloudflare {
			country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
			if country == "" {
				country = "-"
			}
			cfRay := strings.TrimSpace(r.Header.Get("CF-Ray"))
			if cfRay == "" {
				cfRay = "-"
			}
			attrs = append(attrs,
				slog.String("country", country),
				slog.String("cf_ray", cfRay),
			)
		}
		attrs = append(attrs,
			slog.String("user", meta.User),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrapped.statusCode),
			slog.Int64("bytes", wrapped.bytes),
			slog.Float64("duration_ms", durationMs),
		)
		if meta.HasFile {
			attrs = append(attrs, slog.Int64("file_size", meta.FileSize))
		}

		logger.LogAttrs(r.Context(), meta.Level, meta.Msg, attrs...)
	})
}

// sessionMiddleware: Cookieのセッションを検証し、未ログインならログイン画面へ
func sessionMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user := sessions.Get(cookie.Value)
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if meta := getRequestMeta(r.Context()); meta != nil {
			meta.User = user.User
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next(w, r.WithContext(ctx))
	}
}

// loginHandler: GETでフォーム表示、POSTで認証してセッションCookieを発行
func loginHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tmpl.ExecuteTemplate(w, "login.html", struct{ Error string }{})

	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		userInput := r.FormValue("user")
		u := cfg.findUser(userInput, r.FormValue("pass"))
		if u == nil {
			if meta := getRequestMeta(r.Context()); meta != nil {
				meta.Msg = "ログイン失敗"
				meta.Level = slog.LevelWarn
				if userInput != "" {
					meta.User = userInput
				}
			}
			tmpl.ExecuteTemplate(w, "login.html", struct{ Error string }{
				Error: "Invalid username or password",
			})
			return
		}

		ttl := time.Duration(cfg.SessionDays) * 24 * time.Hour
		sid, err := sessions.Create(u, ttl)
		if err != nil {
			http.Error(w, "could not make session", http.StatusInternalServerError)
			return
		}
		if meta := getRequestMeta(r.Context()); meta != nil {
			meta.Msg = "ログイン成功"
			meta.User = u.User
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    sid,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(ttl.Seconds()),
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)

	default:
		http.Error(w, "method is not allowed", http.StatusMethodNotAllowed)
	}
}

// indexHandler: ユーザーのdirectory配下のindex.mdをHTML変換して表示
func indexHandler(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userCtxKey).(*UserConfig)

	mdPath := filepath.Join(cfg.ContentsDir(), u.Directory, "index.md")
	content, err := os.ReadFile(mdPath)
	if err != nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	var buf bytes.Buffer
	if err := goldmark.Convert(content, &buf); err != nil {
		http.Error(w, "Markdown変換エラー", http.StatusInternalServerError)
		return
	}

	data := struct {
		User string
		Body template.HTML
	}{
		User: u.User,
		Body: template.HTML(buf.String()),
	}

	if err := tmpl.ExecuteTemplate(w, "page.html", data); err != nil {
		http.Error(w, "テンプレートエラー", http.StatusInternalServerError)
	}
}

// downloadHandler: ユーザーのdirectory配下のファイルのみ配信(パストラバーサル対策込み)
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userCtxKey).(*UserConfig)

	rawPath := strings.TrimPrefix(r.URL.Path, "/download/")
	filename := filepath.Base(rawPath) // ディレクトリ部分を除去して直下のファイルのみ許可
	if rawPath == "" || filename == "." || filename == ".." || filename == "/" {
		http.Error(w, "ファイルが見つかりません", http.StatusNotFound)
		return
	}

	fullPath := filepath.Join(cfg.ContentsDir(), u.Directory, filename)
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "ファイルが見つかりません", http.StatusNotFound)
		return
	}

	if meta := getRequestMeta(r.Context()); meta != nil {
		meta.Msg = "ダウンロード"
		meta.FileSize = info.Size()
		meta.HasFile = true
	}

	http.ServeFile(w, r, fullPath)
}
