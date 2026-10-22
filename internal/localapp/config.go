// Package localapp provides persistent first-run configuration for the standalone binary.
package localapp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Paths struct{ Config, Data string }

func DefaultPaths() (Paths, error) {
	configRoot, e := os.UserConfigDir()
	if e != nil {
		return Paths{}, e
	}
	dataRoot := configRoot
	if runtime.GOOS == "linux" {
		dataRoot = os.Getenv("XDG_DATA_HOME")
		if dataRoot != "" && !filepath.IsAbs(dataRoot) {
			return Paths{}, errors.New("XDG_DATA_HOME must be an absolute path")
		}
		if dataRoot == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return Paths{}, err
			}
			dataRoot = filepath.Join(home, ".local", "share")
		}
	}
	return Paths{Config: filepath.Join(configRoot, "openslot", "config.env"), Data: filepath.Join(dataRoot, "openslot")}, nil
}

// Resolve preserves cwd-based deployments and environment-only containers.
// An explicit path is always authoritative and never silently replaced.
func Resolve(explicit string) (string, error) {
	if explicit == "" {
		explicit = os.Getenv("OPENSLOT_CONFIG")
	}
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if _, e := os.Stat(".env"); e == nil {
		return filepath.Abs(".env")
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	p, e := DefaultPaths()
	if e != nil {
		if hasServerEnvironment() {
			return "", nil
		}
		return "", e
	}
	if _, e = os.Stat(p.Config); e == nil {
		return p.Config, nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	if hasServerEnvironment() {
		return "", nil
	}
	return p.Config, nil
}
func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

// SecretStoreEnv names a command that keeps secrets outside the configuration
// file, for example an OS keyring helper. Init runs it as `COMMAND... NAME` with
// the secret on stdin, once per entry of SecretNames, and leaves those keys out
// of config.env. The server then needs them in its environment (a launcher such
// as systemd LoadCredential= or a keyring wrapper supplies them). The command
// must refuse to replace an existing secret, so a second installation can never
// overwrite the key of the first.
const SecretStoreEnv = "OPENSLOT_SECRET_STORE"

// SecretNames maps each generated secret to the name Init stores it under.
var SecretNames = map[string]string{
	"CALNODE_ENCRYPTION_KEY":  "openslot-calnode-encryption-key",
	"CALNODE_RECOVERY_SECRET": "openslot-calnode-recovery-secret",
}

// storeSecrets hands each generated secret to the SecretStoreEnv command and
// removes it from values. It returns the comment lines that replace them.
func storeSecrets(command string, values map[string]string) ([]string, error) {
	argv := strings.Fields(command)
	if len(argv) == 0 {
		return nil, nil
	}
	var notes []string
	for _, key := range []string{"CALNODE_ENCRYPTION_KEY", "CALNODE_RECOVERY_SECRET"} {
		if os.Getenv(key) != "" {
			return nil, fmt.Errorf("%s is already set in the environment (an existing installation's key); unset it before creating a new installation with %s", key, SecretStoreEnv)
		}
		name := SecretNames[key]
		cmd := exec.Command(argv[0], append(argv[1:], name)...)
		cmd.Stdin = strings.NewReader(values[key])
		var stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = nil, &stderr
		if e := cmd.Run(); e != nil {
			return nil, fmt.Errorf("%s %s: %v: %s", argv[0], name, e, strings.TrimSpace(stderr.String()))
		}
		delete(values, key)
		notes = append(notes, fmt.Sprintf("# %s is kept outside this file (%s stored it as %s); supply it in the environment.", key, argv[0], name))
	}
	return notes, nil
}

// Init creates configuration once. Existing credentials are never overwritten.
func Init(path, data string) (bool, error) {
	if _, e := os.Stat(path); e == nil {
		return false, nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return false, e
	}
	if data == "" {
		p, e := DefaultPaths()
		if e != nil {
			return false, e
		}
		data = p.Data
	}
	data, e := filepath.Abs(data)
	if e != nil {
		return false, e
	}
	// Refuse to invent a new encryption key for an existing installation.
	if entries, err := os.ReadDir(data); err == nil && len(entries) > 0 {
		return false, fmt.Errorf("data directory already contains files: restore its original configuration or choose an empty directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	key, e := randomSecret()
	if e != nil {
		return false, e
	}
	recovery, e := randomSecret()
	if e != nil {
		return false, e
	}
	values := map[string]string{"PORT": "8788", "BIND_HOST": "127.0.0.1", "BASE_URL": "http://127.0.0.1:8788", "DATABASE_URL": "sqlite://" + filepath.Join(data, "openslot.db"), "DATA_DIR": data, "EMAIL_FROM_NAME": "OpenSlot", "CALNODE_ENCRYPTION_KEY": key, "CALNODE_RECOVERY_SECRET": recovery}
	notes, e := storeSecrets(os.Getenv(SecretStoreEnv), values)
	if e != nil {
		return false, e
	}
	raw, e := godotenv.Marshal(values)
	if e != nil {
		return false, e
	}
	// godotenv expands unescaped dollars inside quoted values when reading.
	raw = strings.ReplaceAll(raw, "$", "\\$")
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return false, e
	}
	if e = os.MkdirAll(data, 0700); e != nil {
		return false, e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".openslot-config-*")
	if e != nil {
		return false, e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	header := "# OpenSlot configuration. Keep this file with your data backups.\n"
	for _, n := range notes {
		header += n + "\n"
	}
	if _, e = f.WriteString(header + raw + "\n"); e != nil {
		return false, e
	}
	if e = f.Sync(); e != nil {
		return false, e
	}
	if e = f.Close(); e != nil {
		return false, e
	}
	// Link publishes the complete file atomically and refuses to replace a
	// configuration created concurrently by another process.
	if e = os.Link(f.Name(), path); errors.Is(e, os.ErrExist) {
		return false, nil
	} else if e != nil {
		return false, e
	}
	return true, nil
}

// Load does not change cwd. Relative storage paths in the file are anchored to it;
// explicit environment overrides retain their original cwd-relative semantics.
func Load(path string) error {
	path, e := filepath.EvalSymlinks(path)
	if e != nil {
		return fmt.Errorf("resolve configuration: %w", e)
	}
	path, e = filepath.Abs(path)
	if e != nil {
		return e
	}
	values, e := godotenv.Read(path)
	if e != nil {
		return fmt.Errorf("read configuration: %w", e)
	}
	dir := filepath.Dir(path)
	for key, value := range values {
		if _, set := os.LookupEnv(key); set {
			continue
		}
		switch key {
		case "DATA_DIR":
			if value != "" && !filepath.IsAbs(value) {
				value = filepath.Join(dir, value)
			}
		case "DATABASE_URL":
			file := strings.TrimPrefix(value, "sqlite://")
			if strings.HasPrefix(file, "file:") {
				uri, err := url.Parse(file)
				if err != nil {
					return fmt.Errorf("DATABASE_URL: %w", err)
				}
				if uri.Opaque != "" && uri.Opaque != ":memory:" && uri.Query().Get("mode") != "memory" {
					decoded, err := url.PathUnescape(uri.Opaque)
					if err != nil {
						return fmt.Errorf("DATABASE_URL: %w", err)
					}
					uri.Opaque = ""
					uri.Path = filepath.Join(dir, decoded)
					value = uri.String()
				}
			} else if file != "" && file != ":memory:" && !filepath.IsAbs(file) {
				value = "sqlite://" + filepath.Join(dir, file)
			}
		}
		if e = os.Setenv(key, value); e != nil {
			return e
		}
	}
	return nil
}
func ValidatePort(port string) error {
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return errors.New("port must be a number between 1 and 65535")
	}
	return nil
}

func hasServerEnvironment() bool {
	for _, key := range []string{"DATABASE_URL", "CALNODE_ENCRYPTION_KEY", "BASE_URL", "PORT"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}
