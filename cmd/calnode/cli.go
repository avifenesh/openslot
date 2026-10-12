package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/calnode/calnode/internal/buildinfo"
	"github.com/calnode/calnode/internal/config"
	"github.com/calnode/calnode/internal/licenseinfo"
	"github.com/calnode/calnode/internal/localapp"
	"github.com/joho/godotenv"
)

const helpText = `OpenSlot: self-hosted scheduling in one executable.

Usage:
  openslot [serve] [--config FILE] [--port PORT] [--bind ADDRESS] [--base-url URL]
  openslot init [--config FILE] [--data-dir DIRECTORY]
  openslot doctor [--config FILE] [--url URL]
  openslot service install [--config FILE] [--start]
  openslot service start|stop|restart|status|uninstall
  openslot agent --engine codex|claude|webhook [options]
  openslot mcp [--config FILE]
  openslot reset-admin EMAIL NEW-PASSWORD [--config FILE]
  openslot rotate-key NEW-SECRET [--config FILE]
  openslot recover-key NEW-SECRET [--config FILE]
  openslot version [--json]
  openslot licenses

Run openslot to create persistent local configuration and start the scheduler.
Open http://127.0.0.1:8788/admin/ and create the owner account.
No Go, Node.js, Python, database server, or model subscription is required.

Configuration: --config, OPENSLOT_CONFIG, .env in the current directory,
then the user configuration directory. Existing environment variables win.
Existing environment-only deployments retain their configured storage and port.
Use openslot service install --start for automatic startup on Linux.
`

func failCLI(err error) { fmt.Fprintln(os.Stderr, "openslot:", err); os.Exit(1) }
func extractConfig(args []string) (string, []string, error) {
	var path string
	rest := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--config" {
			if i+1 == len(args) || strings.HasPrefix(args[i+1], "--") {
				return "", nil, errors.New("--config requires a file path")
			}
			i++
			path = args[i]
		} else if strings.HasPrefix(a, "--config=") {
			path = strings.TrimPrefix(a, "--config=")
			if path == "" {
				return "", nil, errors.New("--config requires a file path")
			}
		} else {
			rest = append(rest, a)
		}
	}
	return path, rest, nil
}
func prepareConfig(explicit string, create bool, data string) (string, error) {
	path, e := localapp.Resolve(explicit)
	if e != nil {
		return "", e
	}
	if path == "" {
		if data != "" {
			return "", errors.New("--data-dir requires a configuration file; supply --config")
		}
		return "", nil
	}
	if _, e = os.Stat(path); errors.Is(e, os.ErrNotExist) && create {
		// An explicit missing config is a typo for serve, not permission to create a
		// new workspace. Only `init` may create a caller-selected config file.
		_, e = localapp.Init(path, data)
		if e != nil {
			return "", e
		}
	} else if e != nil {
		return "", fmt.Errorf("configuration does not exist; run openslot init first: %w", e)
	}
	if e = localapp.Load(path); e != nil {
		return "", e
	}
	return path, nil
}
func runCLI(args []string) {
	command := "serve"
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h", "help":
			fmt.Print(helpText)
			return
		case "licenses":
			if len(args) > 1 {
				failCLI(errors.New("licenses takes no arguments"))
			}
			fmt.Print(licenseinfo.Project, licenseinfo.Dependencies, licenseinfo.Vendored)
			return
		case "--version", "version":
			runVersion(args[1:])
			return
		}
		if !strings.HasPrefix(args[0], "-") {
			command = args[0]
			args = args[1:]
		}
	}
	if command == "agent" {
		// Keep checkout-based agent credentials compatible without exporting the
		// server's unrelated encryption, mail, and provider secrets to workers.
		if e := loadAgentEnvironment(".env"); e != nil {
			failCLI(e)
		}
		runAgent(args)
		return
	}
	explicit, args, e := extractConfig(args)
	if e != nil {
		failCLI(e)
	}
	switch command {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		port := fs.String("port", "", "listen port")
		bind := fs.String("bind", "", "listen address")
		base := fs.String("base-url", "", "public HTTPS URL, or local HTTP URL")
		fs.Usage = func() { fmt.Print(helpText) }
		if e = fs.Parse(args); errors.Is(e, flag.ErrHelp) {
			return
		} else if e != nil {
			failCLI(e)
		}
		if fs.NArg() > 0 {
			failCLI(errors.New("unexpected serve argument; use openslot --help"))
		}
		if *port != "" {
			if e = localapp.ValidatePort(*port); e != nil {
				failCLI(e)
			}
		}
		if *base != "" {
			if e = validServerURL(*base); e != nil {
				failCLI(e)
			}
		}
		_, e = prepareConfig(explicit, explicit == "" && os.Getenv("OPENSLOT_CONFIG") == "", "")
		if e != nil {
			failCLI(e)
		}
		cfg := config.Load()
		if cfg.EncryptionKey == "" {
			failCLI(errors.New("CALNODE_ENCRYPTION_KEY is missing; initialize a new installation or restore its original configuration"))
		}
		oldBase := cfg.BaseURL
		if *port != "" {
			cfg.Port = *port
			if *base == "" && oldBase == "http://127.0.0.1:8788" {
				cfg.BaseURL = "http://127.0.0.1:" + *port
			}
		}
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "bind" {
				cfg.BindHost = *bind
			}
		})
		if *base != "" {
			cfg.BaseURL = strings.TrimRight(*base, "/")
			cfg.CookieSecure = strings.HasPrefix(cfg.BaseURL, "https://")
		}
		if cfg.PublicBaseURL == oldBase {
			cfg.PublicBaseURL = cfg.BaseURL
		}
		cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
		cfg.PublicBaseURL = strings.TrimRight(cfg.PublicBaseURL, "/")
		if e = localapp.ValidatePort(cfg.Port); e != nil {
			failCLI(e)
		}
		if e = validServerURL(cfg.BaseURL); e != nil {
			failCLI(e)
		}
		if e = validServerURL(cfg.PublicBaseURL); e != nil {
			failCLI(fmt.Errorf("PUBLIC_BASE_URL: %w", e))
		}
		if e = os.MkdirAll(cfg.DataDir, 0700); e != nil {
			failCLI(e)
		}
		runServer(cfg)
	case "init":
		fs := flag.NewFlagSet("init", flag.ContinueOnError)
		data := fs.String("data-dir", "", "empty directory for SQLite and uploads")
		if e = fs.Parse(args); errors.Is(e, flag.ErrHelp) {
			return
		} else if e != nil {
			failCLI(e)
		}
		if fs.NArg() > 0 {
			failCLI(errors.New("unexpected init argument"))
		}
		path, e := localapp.Resolve(explicit)
		if e != nil {
			failCLI(e)
		}
		if path == "" {
			failCLI(errors.New("environment-based deployment detected; use --config to create a separate installation"))
		}
		if _, e = localapp.Init(path, *data); e != nil {
			failCLI(e)
		}
		fmt.Println("Configuration ready:", path)
		fmt.Println("Start with: openslot serve --config", path)
	case "doctor":
		fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
		endpoint := fs.String("url", "", "server URL to check")
		if e = fs.Parse(args); errors.Is(e, flag.ErrHelp) {
			return
		} else if e != nil {
			failCLI(e)
		}
		if fs.NArg() > 0 {
			failCLI(errors.New("unexpected doctor argument"))
		}
		path, e := prepareConfig(explicit, false, "")
		if e != nil {
			failCLI(e)
		}
		cfg := config.Load()
		if *endpoint == "" {
			*endpoint = cfg.BaseURL
		}
		if e = validServerURL(*endpoint); e != nil {
			failCLI(e)
		}
		fmt.Println("OpenSlot", buildinfo.Get().Version)
		if path != "" {
			fmt.Println("Configuration:", path)
		} else {
			fmt.Println("Configuration: environment")
		}
		fmt.Println("Data directory:", cfg.DataDir)
		if cfg.EncryptionKey == "" {
			failCLI(errors.New("persistent encryption key is missing"))
		}
		if cfg.RecoverySecret == "" {
			fmt.Println("Recovery key: not configured")
		} else {
			fmt.Println("Recovery key: configured")
		}
		client := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, e := client.Get(strings.TrimRight(*endpoint, "/") + "/readyz")
		if e != nil {
			failCLI(errors.New("server is not reachable; start openslot or check the URL"))
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			failCLI(fmt.Errorf("server is not ready (HTTP %d)", res.StatusCode))
		}
		var ready struct {
			Status  string         `json:"status"`
			Version buildinfo.Info `json:"version"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 65536)).Decode(&ready); err != nil || ready.Status != "ok" || ready.Version.Version == "" {
			failCLI(errors.New("endpoint did not return an OpenSlot readiness response"))
		}
		if ready.Version.Version != buildinfo.Get().Version {
			failCLI(fmt.Errorf("server runs %s but this executable is %s; restart the service after upgrading", ready.Version.Version, buildinfo.Get().Version))
		}
		fmt.Println("Server: ready (" + ready.Version.Version + ")")
		fmt.Println("Admin:", strings.TrimRight(*endpoint, "/")+"/admin/")
	case "service":
		if e = runService(args, explicit); e != nil {
			failCLI(e)
		}
	case "mcp", "reset-admin", "rotate-key", "recover-key":
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			fmt.Print(helpText)
			return
		}
		if command == "mcp" && len(args) > 0 {
			failCLI(errors.New("mcp accepts only --config"))
		}
		if _, e = prepareConfig(explicit, false, ""); e != nil {
			failCLI(e)
		}
		switch command {
		case "mcp":
			runMCPStdio(args)
		case "reset-admin":
			runResetAdmin(args)
		case "rotate-key":
			runRotateKey(args)
		case "recover-key":
			runRecoverKey(args)
		}
	default:
		failCLI(fmt.Errorf("unknown command %q; use openslot --help", command))
	}
}
func runVersion(args []string) {
	if len(args) == 0 {
		b := buildinfo.Get()
		fmt.Printf("openslot %s (%s)\n", b.Version, b.Commit)
		return
	}
	if len(args) == 1 && args[0] == "--json" {
		json.NewEncoder(os.Stdout).Encode(buildinfo.Get())
		return
	}
	failCLI(errors.New("usage: openslot version [--json]"))
}
func validServerURL(value string) error {
	u, e := url.Parse(value)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("base URL must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	return nil
}

func loadAgentEnvironment(path string) error {
	values, err := godotenv.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read agent configuration: %w", err)
	}
	for _, key := range []string{"OPENSLOT_API_KEY", "OPENSLOT_WEBHOOK_TOKEN"} {
		if _, exists := os.LookupEnv(key); !exists {
			if value, present := values[key]; present {
				if err := os.Setenv(key, value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
