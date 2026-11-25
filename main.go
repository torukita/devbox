package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
)

//go:embed templates/*.html
var templateFS embed.FS

var (
	cfg      *Config
	tmpl     *template.Template
	sessions *SessionStore
)

var version = "0.0.0"

var cli struct {
	Dir     string           `arg:"" optional:"" default:"." help:"devbox.toml と contents/ を含む作業ディレクトリ"`
	Version kong.VersionFlag `short:"v" help:"バージョンを表示して終了"`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("devbox"),
		kong.Description("特定の相手に一時的にファイルを共有するための軽量Webサーバーです。"),
		kong.Vars{"version": version},
		kong.UsageOnError(),
	)

	if err := run(cli.Dir); err != nil {
		ctx.FatalIfErrorf(err)
	}
}

func run(targetDir string) error {
	var err error
	cfg, err = LoadConfig(targetDir)
	if err != nil {
		return fmt.Errorf("設定読み込み失敗: %w", err)
	}

	logger, closeLog, err := newLogger(cfg)
	if err != nil {
		return fmt.Errorf("ロガー初期化失敗: %w", err)
	}
	defer closeLog()

	tmpl, err = template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("テンプレート読み込み失敗: %w", err)
	}

	sessions = NewSessionStore()
	mux := newServeMux()

	logOutput := "stdout"
	if cfg.LogFile != "" {
		logOutput = cfg.LogFile
	}
	logger.Info("server started",
		slog.String("version", version),
		slog.String("addr", cfg.Addr),
		slog.String("base_dir", cfg.BaseDir),
		slog.String("contents_dir", cfg.ContentsDir()),
		slog.Int("session_days", cfg.SessionDays),
		slog.String("log_format", cfg.LogFormat),
		slog.String("log_output", logOutput),
		slog.Bool("cloudflare", cfg.Cloudflare),
	)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: loggingMiddleware(logger, cfg.Cloudflare, mux),
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	return runServer(srv, logger, sigCh, func() { signal.Stop(sigCh) })
}

// runServer: HTTPサーバーを起動し、エラー終了または停止シグナル受信時にログを出してグレースフルシャットダウンする
func runServer(srv *http.Server, logger *slog.Logger, sigCh <-chan os.Signal, stopSignals func()) error {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			logger.Error("server stopped", slog.String("error", err.Error()))
			return err
		}
		return nil
	case sig := <-sigCh:
		// 1回目のシグナルを受信したら監視を止め、2回目のCtrl-Cでは即強制終了できるようにする
		if stopSignals != nil {
			stopSignals()
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			logger.Error("server shutdown failed", slog.String("error", err.Error()))
			return err
		}
		logger.Info("server stopped", slog.String("signal", sig.String()))
		return nil
	}
}

func newServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", loginHandler)
	// "/{$}" は "/" にのみ厳密一致するパターン(Go 1.22+)。
	// 単なる "/" だとサブツリーパターン扱いになり、未定義パス(例: /hoge)も
	// 全てここにマッチしてしまうため、それらを404にするために使用する。
	mux.HandleFunc("/{$}", sessionMiddleware(indexHandler))
	mux.HandleFunc("/download/", sessionMiddleware(downloadHandler))
	return mux
}

func newLogger(c *Config) (*slog.Logger, func(), error) {
	var out io.Writer = os.Stdout
	cleanup := func() {}

	if c.LogFile != "" {
		if dir := filepath.Dir(c.LogFile); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, nil, err
			}
		}
		f, err := os.OpenFile(c.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, nil, err
		}
		out = f
		cleanup = func() {
			_ = f.Close()
		}
	}

	var handler slog.Handler
	if c.LogFormat == "json" {
		handler = slog.NewJSONHandler(out, nil)
	} else {
		handler = slog.NewTextHandler(out, nil)
	}
	return slog.New(handler), cleanup, nil
}
