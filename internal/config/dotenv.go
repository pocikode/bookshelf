package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// LoadDotEnv loads variables from path without replacing values already present
// in the process environment. A missing file is not an error, which keeps the
// container path dependent only on variables injected by its runtime.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	values, err := parseDotEnv(file)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s from %s: %w", key, path, err)
		}
	}
	return nil
}

func parseDotEnv(reader io.Reader) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, raw, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !validEnvKey(key) {
			return nil, fmt.Errorf("line %d has an invalid variable name", lineNumber)
		}
		value, err := parseDotEnvValue(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d variable %s: %w", lineNumber, key, err)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return values, nil
}

func validEnvKey(key string) bool {
	if key == "" || !(key[0] == '_' || key[0] >= 'A' && key[0] <= 'Z' || key[0] >= 'a' && key[0] <= 'z') {
		return false
	}
	for i := 1; i < len(key); i++ {
		character := key[i]
		if character != '_' && !(character >= 'A' && character <= 'Z') && !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}

func parseDotEnvValue(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if raw[0] == '\'' {
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", errors.New("unterminated single-quoted value")
		}
		end++
		if err := validDotEnvRemainder(raw[end+1:]); err != nil {
			return "", err
		}
		return raw[1:end], nil
	}
	if raw[0] == '"' {
		end := closingDoubleQuote(raw)
		if end < 0 {
			return "", errors.New("unterminated double-quoted value")
		}
		if err := validDotEnvRemainder(raw[end+1:]); err != nil {
			return "", err
		}
		value, err := strconv.Unquote(raw[:end+1])
		if err != nil {
			return "", errors.New("invalid escape in double-quoted value")
		}
		return value, nil
	}
	for index, character := range raw {
		if character == '#' && index > 0 && (raw[index-1] == ' ' || raw[index-1] == '\t') {
			raw = raw[:index]
			break
		}
	}
	return strings.TrimSpace(raw), nil
}

func closingDoubleQuote(value string) int {
	escaped := false
	for index := 1; index < len(value); index++ {
		if escaped {
			escaped = false
			continue
		}
		if value[index] == '\\' {
			escaped = true
			continue
		}
		if value[index] == '"' {
			return index
		}
	}
	return -1
}

func validDotEnvRemainder(remainder string) error {
	remainder = strings.TrimSpace(remainder)
	if remainder == "" || strings.HasPrefix(remainder, "#") {
		return nil
	}
	return errors.New("unexpected text after quoted value")
}
