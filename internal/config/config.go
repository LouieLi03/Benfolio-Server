package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddress string
	DataDir       string
	BooksDir      string
	APIKey        string
}

func ListenAddress() string {
	if value := os.Getenv("BENFOLIO_LISTEN_ADDR"); value != "" {
		return value
	}
	return ":8080"
}

func Load() (Config, error) {
	cfg := Config{ListenAddress: ListenAddress(), DataDir: os.Getenv("BENFOLIO_DATA_DIR"), BooksDir: os.Getenv("BENFOLIO_BOOKS_DIR")}
	_, port, err := net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		return Config{}, errors.New("BENFOLIO_LISTEN_ADDR 必须为 host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, errors.New("监听端口必须在 1–65535 之间")
	}
	if cfg.DataDir == "" {
		cfg.DataDir = ".local/benfolio-server"
	}
	cfg.DataDir, err = filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("解析状态目录: %w", err)
	}
	if cfg.BooksDir != "" {
		cfg.BooksDir, err = filepath.Abs(cfg.BooksDir)
		if err != nil {
			return Config{}, fmt.Errorf("解析书籍来源目录: %w", err)
		}
		// The source is never created or modified by the scaffold.
		if contains(cfg.DataDir, cfg.BooksDir) || contains(cfg.BooksDir, cfg.DataDir) {
			return Config{}, errors.New("状态目录与书籍来源目录不能互相包含")
		}
	}
	key, keyFile := os.Getenv("BENFOLIO_API_KEY"), os.Getenv("BENFOLIO_API_KEY_FILE")
	if key != "" && keyFile != "" {
		return Config{}, errors.New("API Key 环境变量与文件配置只能选择一种")
	}
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return Config{}, errors.New("无法读取 API Key 文件")
		}
		key = strings.TrimSpace(string(data))
	}
	if len(key) < 32 || len(key) > 512 || strings.ContainsAny(key, " \t\r\n") {
		return Config{}, errors.New("必须配置 32–512 字节且不含空白的 API Key")
	}
	cfg.APIKey = key
	return cfg, nil
}

func contains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
