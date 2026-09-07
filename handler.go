package main

import (
	"bytes"
	"context"
	"html/template"
	"log"
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
	sessionCookieName string = "session_id"
)

// responseWriterWrapper: 出力されたHTTPステータスコードをキャプチャするためのラッパー
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// clientIP: X-Forwarded-Forヘッダー（先頭のIP）があればそれを、なければRemoteAddrを返す
// cloudflaredなどのプロキシ経由の場合、RemoteAddrはプロキシのアドレスになるため
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
}

// loggingMiddleware: すべてのリクエストのアクセスログ（IP、ユーザー名、メソッド、URL、ステータスコード、処理時間）を出力
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		// ログイン中のユーザー名を取得（セッションCookieが存在すれば取得）
		userName := "-"
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if u := sessions.Get(cookie.Value); u != nil {
				userName = u.User
			}
		}

		ip := clientIP(r)

		// ダウンロードリクエストの場合は [DOWNLOAD] として出力
		if strings.HasPrefix(r.URL.Path, "/download/") {
			filename := filepath.Base(strings.TrimPrefix(r.URL.Path, "/download/"))
			log.Printf("[DOWNLOAD] IP: %s | user: %s | file: %s %d (%v)",
				ip,
				userName,
				filename,
				wrapped.statusCode,
				duration,
			)
			return
		}

		// 通常のリクエストは [ACCESS] として出力
		log.Printf("[ACCESS]   IP: %s | user: %s | %s %s %d (%v)",
			ip,
			userName,
			r.Method,
			r.RequestURI,
			wrapped.statusCode,
			duration,
		)
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
		u := cfg.findUser(r.FormValue("user"), r.FormValue("pass"))
		if u == nil {
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

	mdPath := filepath.Join(cfg.DataDir, u.Directory, "index.md")
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

	filename := strings.TrimPrefix(r.URL.Path, "/download/")
	filename = filepath.Base(filename) // ディレクトリ部分を除去して直下のファイルのみ許可

	fullPath := filepath.Join(cfg.DataDir, u.Directory, filename)
	if _, err := os.Stat(fullPath); err != nil {
		http.Error(w, "ファイルが見つかりません", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, fullPath)
}
