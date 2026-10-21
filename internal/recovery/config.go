package recovery

import (
	"bufio"
	"bytes"
	"crypto/sha1" // Telegram protocol auth_key_id, not archive cryptography.
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var configAllowlist = map[string]bool{
	"DRIVE_STORAGE": true, "DRIVE_SECURE_COOKIES": true,
	"TELEGRAM_APP_ID": true, "TELEGRAM_APP_HASH": true,
	"USERNAME_OR_CHANNEL_LINK": true, "TELEGRAM_CHANNEL_ID": true,
	"TELEGRAM_CHANNEL_ACCESS_HASH": true,
}

// ReadConfig accepts literal dotenv assignments; values are never executed or
// expanded. Runtime paths, listen address, and unrelated environment are omitted.
func ReadConfig(raw []byte) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 16384)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" {
			return nil, errors.New("invalid recovery configuration assignment")
		}
		if !configAllowlist[name] {
			continue
		}
		if _, exists := values[name]; exists {
			return nil, errors.New("duplicate recovery configuration assignment")
		}
		if strings.HasPrefix(value, "\"") {
			prefix, err := strconv.QuotedPrefix(value)
			if err != nil {
				return nil, errors.New("invalid quoted recovery configuration")
			}
			tail := strings.TrimSpace(value[len(prefix):])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return nil, errors.New("invalid recovery configuration comment")
			}
			value, err = strconv.Unquote(prefix)
			if err != nil {
				return nil, errors.New("invalid quoted recovery configuration")
			}
		} else if strings.HasPrefix(value, "'") {
			end := strings.IndexByte(value[1:], '\'')
			if end < 0 {
				return nil, errors.New("invalid quoted recovery configuration")
			}
			end++
			tail := strings.TrimSpace(value[end+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return nil, errors.New("invalid recovery configuration comment")
			}
			value = value[1:end]
		} else if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		if len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
			return nil, errors.New("invalid recovery configuration value")
		}
		values[name] = value
	}
	if scanner.Err() != nil {
		return nil, errors.New("recovery configuration exceeds limits")
	}
	return values, nil
}

func configValues(raw []byte, b binding) (map[string]string, error) {
	values, err := ReadConfig(raw)
	if err != nil {
		return nil, err
	}
	if configured := values["DRIVE_STORAGE"]; configured != "" && configured != b.Adapter {
		return nil, errors.New("recovery configuration adapter differs from drive")
	}
	values["DRIVE_STORAGE"] = b.Adapter
	if secure := values["DRIVE_SECURE_COOKIES"]; secure != "" {
		if _, err := strconv.ParseBool(secure); err != nil {
			return nil, errors.New("invalid recovery cookie configuration")
		}
	}
	// Fresh restoration starts on loopback HTTP. Configure HTTPS before changing
	// cookie policy or the bind address in the restored private environment file.
	values["DRIVE_SECURE_COOKIES"] = "false"
	if b.Adapter == "local" {
		for name := range values {
			if strings.HasPrefix(name, "TELEGRAM_") || name == "USERNAME_OR_CHANNEL_LINK" {
				delete(values, name)
			}
		}
		return values, nil
	}
	id, err := strconv.ParseInt(values["TELEGRAM_APP_ID"], 10, 32)
	appHash, hashErr := hex.DecodeString(values["TELEGRAM_APP_HASH"])
	if err != nil || id <= 0 || hashErr != nil || len(appHash) != 16 {
		return nil, errors.New("complete Telegram backup requires valid app configuration")
	}
	if value := values["TELEGRAM_CHANNEL_ID"]; value != "" {
		channel, err := strconv.ParseInt(value, 10, 64)
		if channel < -1000000000000 && channel > -2000000000000 {
			channel = -channel - 1000000000000
		}
		if err != nil || channel != b.ChannelID {
			return nil, errors.New("recovery configuration channel differs from storage binding")
		}
	}
	values["TELEGRAM_CHANNEL_ID"] = strconv.FormatInt(b.ChannelID, 10)
	link := strings.TrimSpace(values["USERNAME_OR_CHANNEL_LINK"])
	hash := values["TELEGRAM_CHANNEL_ACCESS_HASH"]
	if link != "" {
		if hash != "" {
			return nil, errors.New("recovery channel must use link or access hash, not both")
		}
		if err := validateChannelLink(link, b.ChannelID); err != nil {
			return nil, err
		}
		values["USERNAME_OR_CHANNEL_LINK"] = link
	} else if access, err := strconv.ParseInt(hash, 10, 64); err != nil || access == 0 {
		return nil, errors.New("complete Telegram backup requires channel link or access hash")
	}
	return values, nil
}

func validateChannelLink(value string, channelID int64) error {
	invalid := errors.New("recovery channel requires a valid private Telegram invite or matching private message link")
	if len(value) > 2048 || strings.ContainsAny(value, "?#") {
		return invalid
	}
	if strings.HasPrefix(strings.ToLower(value), "t.me/") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Host, "t.me") || u.User != nil || u.RawPath != "" {
		return invalid
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	invite := ""
	switch {
	case len(parts) == 1 && strings.HasPrefix(parts[0], "+"):
		invite = strings.TrimPrefix(parts[0], "+")
	case len(parts) == 2 && parts[0] == "joinchat":
		invite = parts[1]
	case len(parts) == 3 && parts[0] == "c":
		id, idErr := strconv.ParseInt(parts[1], 10, 64)
		message, messageErr := strconv.ParseInt(parts[2], 10, 32)
		if idErr != nil || messageErr != nil || id <= 0 || id != channelID || message <= 0 || strconv.FormatInt(id, 10) != parts[1] || strconv.FormatInt(message, 10) != parts[2] {
			return invalid
		}
		return nil
	}
	if invite == "" || len(invite) > 256 {
		return invalid
	}
	for _, c := range invite {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return invalid
		}
	}
	return nil
}

func encodeConfig(values map[string]string) []byte {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	var text strings.Builder
	text.WriteString("# Private Harbor recovery configuration. Never commit or share.\n")
	for _, name := range names {
		text.WriteString(name + "=" + strconv.Quote(values[name]) + "\n")
	}
	return []byte(text.String())
}

func backupConfig(path string, b binding) ([]byte, error) {
	var raw []byte
	if path != "" {
		var err error
		raw, err = readPrivate(path, 16384)
		if err != nil {
			return nil, err
		}
		defer clear(raw)
	}
	values, err := configValues(raw, b)
	if err != nil {
		return nil, err
	}
	return encodeConfig(values), nil
}

func restoreConfig(scratch, destination string, b binding) error {
	path := filepath.Join(scratch, "recovery.env")
	raw, err := readPrivate(path, 16384)
	if err != nil {
		return err
	}
	defer clear(raw)
	values, err := configValues(raw, b)
	if err != nil {
		return err
	}
	values["DRIVE_ADDR"] = "127.0.0.1:8080"
	values["DRIVE_DATA_DIR"] = destination
	values["DRIVE_KEY_FILE"] = filepath.Join(destination, "content.key")
	values["DRIVE_OWNER_PASSWORD_FILE"] = filepath.Join(destination, "owner-password")
	if b.Adapter == "telegram" {
		values["TELEGRAM_SESSION_FILE"] = filepath.Join(destination, "telegram.session")
	}
	encoded := encodeConfig(values)
	defer clear(encoded)
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		return errors.New("cannot write private restored configuration")
	}
	return nil
}

func validateSession(raw []byte) error {
	var session struct {
		Version int
		Data    struct {
			DC        int
			Addr      string
			AuthKey   []byte
			AuthKeyID []byte
		}
	}
	if json.Unmarshal(raw, &session) != nil || session.Version != 1 || session.Data.DC < 1 || len(session.Data.AuthKey) != 256 || len(session.Data.AuthKeyID) != 8 || len(session.Data.Addr) > 255 {
		return errors.New("invalid persisted Telegram session")
	}
	defer clear(session.Data.AuthKey)
	// gotd v0.162.0 leaves legacy Addr empty and reconnects using DC/config.
	// Its restore path validates auth_key_id against the SHA-1 key fingerprint.
	id := sha1.Sum(session.Data.AuthKey)
	if !bytes.Equal(id[12:], session.Data.AuthKeyID) {
		return errors.New("invalid persisted Telegram session key fingerprint")
	}
	return nil
}
