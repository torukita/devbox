package main

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

type UserConfig struct {
	User      string `toml:"user"`
	Pass      string `toml:"pass"`
	Directory string `toml:"directory"`
}

type Config struct {
	DataDir     string       `toml:"data_dir"`
	Addr        string       `toml:"addr"`
	SessionDays int          `toml:"session_days"`
	Users       []UserConfig `toml:"users"`
}

func LoadConfig(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, err
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	if c.SessionDays <= 0 {
		c.SessionDays = 14
	}
	if len(c.Users) == 0 {
		return nil, fmt.Errorf("設定ファイルにユーザーが定義されていません")
	}
	return &c, nil
}

func (c *Config) findUser(user, pass string) *UserConfig {
	for i := range c.Users {
		if c.Users[i].User == user && c.Users[i].Pass == pass {
			return &c.Users[i]
		}
	}
	return nil
}
