package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const serviceMarker = "# Managed by openslot service install."

func unitQuote(value string, command bool) string {
	value = strings.ReplaceAll(value, "%", "%%")
	if command {
		value = strings.ReplaceAll(value, "$", "$$")
	}
	return strconv.Quote(value)
}
func serviceUnit(executable, configFile string) string {
	return serviceMarker + "\n[Unit]\nDescription=OpenSlot scheduling server\nConditionPathExists=" + unitQuote(configFile, false) + "\n\n[Service]\nType=simple\nWorkingDirectory=" + unitQuote(filepath.Dir(configFile), false) + "\nExecStart=" + unitQuote(executable, true) + " serve --config " + unitQuote(configFile, true) + "\nRestart=on-failure\nRestartSec=5s\nTimeoutStopSec=95s\n\n[Install]\nWantedBy=default.target\n"
}
func systemctlEnv() []string {
	env := os.Environ()
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
		if info, e := os.Stat(dir); e == nil && info.IsDir() {
			env = append(env, "XDG_RUNTIME_DIR="+dir)
		}
	}
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		if _, e := os.Stat(filepath.Join(dir, "bus")); e == nil {
			env = append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(dir, "bus"))
		}
	}
	return env
}
func callSystemctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
	cmd.Env = systemctlEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("user service operation failed: %w", e)
	}
	return nil
}
func runService(args []string, explicit string) error {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		fmt.Print(helpText)
		return nil
	}
	if runtime.GOOS != "linux" {
		return errors.New("service management uses systemd on Linux; run openslot serve in your platform's service manager")
	}
	if _, e := exec.LookPath("systemctl"); e != nil {
		return errors.New("systemctl is not available; run openslot serve in your service manager")
	}
	configRoot, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	unit := filepath.Join(configRoot, "systemd", "user", "openslot.service")
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		start := fs.Bool("start", false, "start or restart the server now")
		replace := fs.Bool("replace", false, "replace an existing unmanaged openslot.service, preserving a .bak copy")
		if e = fs.Parse(args[1:]); errors.Is(e, flag.ErrHelp) {
			return nil
		} else if e != nil {
			return e
		}
		if fs.NArg() > 0 {
			return errors.New("unexpected service install argument")
		}
		path, e := prepareConfig(explicit, explicit == "" && os.Getenv("OPENSLOT_CONFIG") == "", "")
		if e != nil {
			return e
		}
		if path == "" {
			return errors.New("service installation requires persistent configuration; use openslot init --config FILE first")
		}
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		executable, e = filepath.EvalSymlinks(executable)
		if e != nil {
			return e
		}
		old, e := os.ReadFile(unit)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e == nil && !strings.HasPrefix(string(old), serviceMarker) {
			if !*replace {
				return errors.New("an unmanaged openslot.service already exists; review it and use --replace to replace it")
			}
			if _, e = os.Stat(unit + ".bak"); e == nil {
				return errors.New("a service backup already exists; preserve it elsewhere before replacing the unit")
			}
			if e = os.WriteFile(unit+".bak", old, 0600); e != nil {
				return e
			}
		}
		if e = os.MkdirAll(filepath.Dir(unit), 0700); e != nil {
			return e
		}
		tmp, e := os.CreateTemp(filepath.Dir(unit), ".openslot-service-*")
		if e != nil {
			return e
		}
		defer os.Remove(tmp.Name())
		if _, e = tmp.WriteString(serviceUnit(executable, path)); e != nil {
			tmp.Close()
			return e
		}
		if e = tmp.Close(); e != nil {
			return e
		}
		if e = os.Rename(tmp.Name(), unit); e != nil {
			return e
		}
		if e = callSystemctl("daemon-reload"); e != nil {
			return e
		}
		if e = callSystemctl("enable", "openslot.service"); e != nil {
			return e
		}
		if *start {
			if e = callSystemctl("restart", "openslot.service"); e != nil {
				return e
			}
		}
		fmt.Println("User service installed:", unit)
		fmt.Println("Starts at sign-in. Headless boot startup requires systemd user lingering.")
		return nil
	case "start", "stop", "restart":
		if len(args) != 1 {
			return errors.New("unexpected service argument")
		}
		return callSystemctl(args[0], "openslot.service")
	case "status":
		if len(args) != 1 {
			return errors.New("unexpected service argument")
		}
		return callSystemctl("status", "--no-pager", "openslot.service")
	case "uninstall":
		if len(args) != 1 {
			return errors.New("unexpected service uninstall argument")
		}
		raw, e := os.ReadFile(unit)
		if errors.Is(e, os.ErrNotExist) {
			fmt.Println("No OpenSlot user service is installed.")
			return nil
		}
		if e != nil {
			return e
		}
		if !strings.HasPrefix(string(raw), serviceMarker) {
			return errors.New("refusing to remove an unmanaged service; remove it through your service manager")
		}
		if e = callSystemctl("disable", "--now", "openslot.service"); e != nil {
			return e
		}
		if e = os.Remove(unit); e != nil {
			return e
		}
		if e = callSystemctl("daemon-reload"); e != nil {
			return e
		}
		fmt.Println("Service removed. Configuration and data were preserved.")
		return nil
	default:
		return fmt.Errorf("unknown service action %q", args[0])
	}
}
