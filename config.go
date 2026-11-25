package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type UserConfig struct {
	User      string `toml:"user"`
	Pass      string `toml:"pass"`
	Directory string `toml:"directory"`
}

type Config struct {
	BaseDir     string       `toml:"-"`
	Addr        string       `toml:"addr"`
	SessionDays int          `toml:"session_days"`
	LogFile     string       `toml:"log_file"`
	LogFormat   string       `toml:"log_format"`
	Cloudflare  bool         `toml:"cloudflare"`
	Users       []UserConfig `toml:"users"`
}

func LoadConfig(dir string) (*Config, error) {
	if dir == "" {
		dir = "."
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("ディレクトリの確認に失敗しました (%s): %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("設定ファイルではなくディレクトリを指定してください: %s", dir)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("ディレクトリの絶対パス解決に失敗しました: %w", err)
	}

	configPath := filepath.Join(absDir, "devbox.toml")
	var c Config
	if _, err := toml.DecodeFile(configPath, &c); err != nil {
		return nil, err
	}
	c.BaseDir = absDir

	if c.Addr == "" {
		c.Addr = ":8080"
	}
	if c.SessionDays <= 0 {
		c.SessionDays = 14
	}
	if c.LogFormat == "" {
		c.LogFormat = "text"
	}
	if c.LogFormat != "text" && c.LogFormat != "json" {
		return nil, fmt.Errorf("log_format は text または json を指定してください: %s", c.LogFormat)
	}
	if c.LogFile != "" && !filepath.IsAbs(c.LogFile) {
		return nil, fmt.Errorf("log_file は絶対パスで指定してください: %s", c.LogFile)
	}
	if len(c.Users) == 0 {
		return nil, fmt.Errorf("設定ファイルにユーザーが定義されていません")
	}
	for i, u := range c.Users {
		if u.User == "" {
			return nil, fmt.Errorf("users[%d].user が空です", i)
		}
		if !isSafeDirName(u.Directory) {
			return nil, fmt.Errorf("users[%d].directory が不正です: %q", i, u.Directory)
		}
	}
	return &c, nil
}

// isSafeDirName: contents配下の単一ディレクトリ名として安全か検証する
func isSafeDirName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return filepath.Base(name) == name
}

// ContentsDir: 公開コンテンツのルートディレクトリ(<BaseDir>/contents)を返す
func (c *Config) ContentsDir() string {
	return filepath.Join(c.BaseDir, "contents")
}

func (c *Config) findUser(user, pass string) *UserConfig {
	for i := range c.Users {
		if c.Users[i].User == user && c.Users[i].Pass == pass {
			return &c.Users[i]
		}
	}
	return nil
}
