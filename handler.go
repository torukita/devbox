package main

import (
	"bytes"
	"context"
	"html/template"
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
			http.Error(w, "不正なリクエスト", http.StatusBadRequest)
			return
		}
		u := cfg.findUser(r.FormValue("user"), r.FormValue("pass"))
		if u == nil {
			tmpl.ExecuteTemplate(w, "login.html", struct{ Error string }{
				Error: "ユーザー名またはパスワードが違います",
			})
			return
		}

		ttl := time.Duration(cfg.SessionDays) * 24 * time.Hour
		sid, err := sessions.Create(u, ttl)
		if err != nil {
			http.Error(w, "セッション作成エラー", http.StatusInternalServerError)
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
		http.Error(w, "許可されていないメソッド", http.StatusMethodNotAllowed)
	}
}

// indexHandler: ユーザーのdirectory配下のindex.mdをHTML変換して表示
func indexHandler(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userCtxKey).(*UserConfig)

	mdPath := filepath.Join(cfg.DataDir, u.Directory, "index.md")
	content, err := os.ReadFile(mdPath)
	if err != nil {
		http.Error(w, "ページが見つかりません", http.StatusNotFound)
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
