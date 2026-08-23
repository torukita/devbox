package main

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
)

//go:embed templates/*.html
var templateFS embed.FS

var (
	cfg      *Config
	tmpl     *template.Template
	sessions *SessionStore
)

func main() {
	configPath := "devbox.toml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	var err error
	cfg, err = LoadConfig(configPath)
	if err != nil {
		log.Fatalf("設定読み込み失敗: %v", err)
	}

	tmpl, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatalf("テンプレート読み込み失敗: %v", err)
	}

	sessions = NewSessionStore()

	mux := http.NewServeMux()
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/", sessionMiddleware(indexHandler))
	mux.HandleFunc("/download/", sessionMiddleware(downloadHandler))

	log.Printf("起動: %s (data_dir=%s, session_days=%d)", cfg.Addr, cfg.DataDir, cfg.SessionDays)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
