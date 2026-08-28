// Package probe 是所有工具共用的「探针」：读配置文件、找可执行文件、打网关。
//
// 它不知道任何具体工具的语义——那些在 harness/<tool> 里。
package probe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// Home 返回用户主目录；AIVET_HOME 可覆盖（测试 / 多账号用）。
func Home() string {
	if h := os.Getenv("AIVET_HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// Exists 判断路径是否存在。
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadJSON 读取 JSON 文件到 v；文件不存在返回 os.ErrNotExist。
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("JSON 解析失败：%w", err)
	}
	return nil
}

// ReadTOML 读取 TOML 文件到 v。
func ReadTOML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := toml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("TOML 解析失败：%w", err)
	}
	return nil
}

// ReadYAML 读取 YAML 文件到 v。
func ReadYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("YAML 解析失败：%w", err)
	}
	return nil
}

// IsNotExist 判断错误是否为「文件不存在」。
func IsNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }

// Backup 在改任何用户文件前先复制一份 <path>.aivet-bak-<时间戳>。
// 文件不存在时不做事、不报错。返回备份路径（可能为空）。
func Backup(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	bak := fmt.Sprintf("%s.aivet-bak-%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(bak, b, 0o600); err != nil {
		return "", err
	}
	return bak, nil
}

// WriteFile 先建目录再写文件；凭证类文件统一 0600。
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// WriteJSON 以两空格缩进写 JSON。
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(path, append(b, '\n'))
}

// WriteYAML 写 YAML（两空格缩进）。
func WriteYAML(path string, v any) error {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	_ = enc.Close()
	return WriteFile(path, []byte(sb.String()))
}

// ParseDotenv 解析 KEY=VALUE 形式的 .env 文件（支持 # 注释、引号）。
func ParseDotenv(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// UpsertDotenv 在 .env 里设置/替换一个键，其他行原样保留。
func UpsertDotenv(path, key, value string) error {
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(b) == 0 {
		lines = nil
	}
	found := false
	for i, line := range lines {
		t := strings.TrimPrefix(strings.TrimSpace(line), "export ")
		if strings.HasPrefix(t, key+"=") {
			lines[i] = key + "=" + value
			found = true
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	return WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"))
}
