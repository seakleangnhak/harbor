package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Environment variables override the optional local file. Values are never executed.
// ponytail: literal dotenv values only; add a dotenv library if interpolation becomes necessary.
func loadEnvironment() error {
	path := env("DRIVE_ENV_FILE", ".env")
	if path == "-" {
		return nil
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && os.Getenv("DRIVE_ENV_FILE") == "" {
		return nil
	}
	if err != nil {
		return errors.New("cannot read environment file")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("environment file must be a regular file with mode 0600; run chmod 600 .env")
	}
	f, err := os.Open(path)
	if err != nil {
		return errors.New("cannot read environment file")
	}
	defer f.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		name, value, ok := strings.Cut(text, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		valid := ok && name != ""
		for i, c := range name {
			valid = valid && (c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9')
		}
		if !valid {
			return fmt.Errorf("invalid environment assignment on line %d", line)
		}
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			quote := value[0]
			end := strings.IndexByte(value[1:], quote) + 2
			if quote == '"' {
				prefix, quoteErr := strconv.QuotedPrefix(value)
				if quoteErr != nil {
					return fmt.Errorf("invalid quoted environment value on line %d", line)
				}
				end = len(prefix)
			}
			if end < 2 || (strings.TrimSpace(value[end:]) != "" && !strings.HasPrefix(strings.TrimSpace(value[end:]), "#")) {
				return fmt.Errorf("invalid quoted environment value on line %d", line)
			}
			value = value[:end]
			if quote == '"' {
				value, err = strconv.Unquote(value)
				if err != nil {
					return fmt.Errorf("invalid quoted environment value on line %d", line)
				}
			} else {
				value = value[1 : len(value)-1]
			}
		} else if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		values[name] = value
	}
	if scanner.Err() != nil {
		return errors.New("cannot read environment file")
	}
	for name, value := range values {
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, value); err != nil {
				return errors.New("cannot load environment variable")
			}
		}
	}
	return nil
}
