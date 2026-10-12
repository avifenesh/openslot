package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLIProcess(t *testing.T) {
	if os.Getenv("OPENSLOT_CLI_TEST") != "1" {
		return
	}
	for i, v := range os.Args {
		if v == "--" {
			runCLI(os.Args[i+1:])
			os.Exit(0)
		}
	}
	os.Exit(2)
}
func cliProcess(t *testing.T, root string, extra []string, args ...string) *exec.Cmd {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("standalone integration fixture uses Linux XDG and systemd paths")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIProcess$", "--"}, args...)...)
	cmd.Dir = root
	cmd.Env = append([]string{"OPENSLOT_CLI_TEST=1", "PATH=" + os.Getenv("PATH"), "HOME=" + root, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_DATA_HOME=" + filepath.Join(root, "data"), "TMPDIR=" + root}, extra...)
	return cmd
}
func TestCLIHelpVersionAndUnknownDoNotStartServer(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"licenses"}, {"version"}, {"version", "--json"}, {"serve", "--help"}, {"init", "--help"}, {"agent", "--help"}} {
		out, e := cliProcess(t, root, nil, args...).CombinedOutput()
		if e != nil {
			t.Fatalf("%v: %s", args, out)
		}
		if len(out) == 0 {
			t.Fatal("empty help/version output")
		}
	}
	if _, e := os.Stat(filepath.Join(root, "config")); !os.IsNotExist(e) {
		t.Fatal("help/version created configuration")
	}
	out, e := cliProcess(t, root, nil, "nonsense").CombinedOutput()
	if e == nil || !strings.Contains(string(out), "unknown command") {
		t.Fatal("unknown command did not fail safely", e)
	}
}
func TestCLIFirstRunAndRestartUseSameData(t *testing.T) {
	root := t.TempDir()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	url := "http://127.0.0.1:" + fmt.Sprint(port)
	var configBefore []byte
	var key string
	for i := 0; i < 2; i++ {
		cmd := cliProcess(t, root, nil, "serve", "--port", fmt.Sprint(port))
		log, e := os.Create(filepath.Join(root, fmt.Sprintf("run-%d.log", i)))
		if e != nil {
			t.Fatal(e)
		}
		cmd.Stdout = log
		cmd.Stderr = log
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			log.Close()
		})
		deadline := time.Now().Add(10 * time.Second)
		var ready bool
		for time.Now().Before(deadline) {
			response, e := http.Get(url + "/readyz")
			if e == nil {
				response.Body.Close()
				if response.StatusCode == 200 {
					ready = true
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !ready {
			t.Fatal("server did not become ready; see temporary test log")
		}
		response, e := http.Get(url + "/v1/auth/status")
		if e != nil {
			t.Fatal(e)
		}
		var status map[string]any
		json.NewDecoder(response.Body).Decode(&status)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("missing bootstrap surface")
		}

		if i == 0 {
			response, e := http.Post(url+"/v1/setup", "application/json", strings.NewReader(`{"name":"Standalone owner","email":"standalone@example.test","timezone":"UTC"}`))
			if e != nil {
				t.Fatal(e)
			}
			var setup struct {
				APIKey string `json:"api_key"`
			}
			e = json.NewDecoder(response.Body).Decode(&setup)
			response.Body.Close()
			if e != nil || response.StatusCode != 201 || setup.APIKey == "" {
				t.Fatal("could not create synthetic owner")
			}
			key = setup.APIKey
		}
		request, _ := http.NewRequest("GET", url+"/v1/users/me", nil)
		request.Header.Set("Authorization", "Bearer "+key)
		response, e = http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		var owner struct {
			Name string `json:"name"`
		}
		e = json.NewDecoder(response.Body).Decode(&owner)
		response.Body.Close()
		if e != nil || response.StatusCode != 200 || owner.Name != "Standalone owner" {
			t.Fatal("owner data did not survive process restart")
		}
		path := filepath.Join(root, "config/openslot/config.env")
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			configBefore = raw
		} else if string(configBefore) != string(raw) {
			t.Fatal("restart replaced persistent configuration")
		}
		out, e := cliProcess(t, root, nil, "doctor", "--url", url).CombinedOutput()
		if e != nil || !strings.Contains(string(out), "Server: ready") {
			t.Fatalf("doctor: %s", out)
		}
		cmd.Process.Signal(os.Interrupt)
		if e = cmd.Wait(); e != nil {
			t.Fatal("clean shutdown failed", e)
		}
		log.Close()
	}
	if _, e = os.Stat(filepath.Join(root, "data/openslot/openslot.db")); e != nil {
		t.Fatal(e)
	}
}
func TestCLIPortConflictFailsBeforeDatabaseIsCreated(t *testing.T) {
	root := t.TempDir()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	out, e := cliProcess(t, root, nil, "serve", "--port", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)).CombinedOutput()
	if e == nil || !strings.Contains(string(out), "cannot listen") {
		t.Fatal("port conflict did not fail", e)
	}
	if _, e = os.Stat(filepath.Join(root, "data/openslot/openslot.db")); !os.IsNotExist(e) {
		t.Fatal("port conflict created a database")
	}
}
func TestServiceUnitKeepsPathsLiteral(t *testing.T) {
	unit := serviceUnit("/home/person/a $name%/openslot", "/home/person/a $name%/config.env")
	if !strings.Contains(unit, `ExecStart="/home/person/a $$name%%/openslot" serve --config "/home/person/a $$name%%/config.env"`) {
		t.Fatal("exec path not escaped")
	}
	if !strings.Contains(unit, `WorkingDirectory="/home/person/a $name%%"`) {
		t.Fatal("working directory not escaped")
	}
	if !strings.Contains(unit, "WantedBy=default.target") || !strings.Contains(unit, "Restart=on-failure") {
		t.Fatal("service lacks persistence/restart")
	}
}

func TestAgentCheckoutCredentialsRemainScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("OPENSLOT_API_KEY=checkout-key\nOPENSLOT_WEBHOOK_TOKEN=checkout-token\nCALNODE_ENCRYPTION_KEY=server-only\n"), 0600)
	for _, key := range []string{"OPENSLOT_API_KEY", "OPENSLOT_WEBHOOK_TOKEN", "CALNODE_ENCRYPTION_KEY"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	t.Setenv("OPENSLOT_WEBHOOK_TOKEN", "environment-token")
	if err := loadAgentEnvironment(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("OPENSLOT_API_KEY") != "checkout-key" || os.Getenv("OPENSLOT_WEBHOOK_TOKEN") != "environment-token" {
		t.Fatal("credentials or precedence lost")
	}
	if _, ok := os.LookupEnv("CALNODE_ENCRYPTION_KEY"); ok {
		t.Fatal("unrelated server secret leaked into agent environment")
	}
}

func TestCLIServiceInstallReplaceAndUninstallPreserveData(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	os.Mkdir(bin, 0700)
	log := filepath.Join(root, "systemctl.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SYSTEMCTL_LOG\"\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "SYSTEMCTL_LOG=" + log}
	unit := filepath.Join(root, "config", "systemd", "user", "openslot.service")
	os.MkdirAll(filepath.Dir(unit), 0700)
	original := []byte("[Service]\nExecStart=/old/server\n")
	os.WriteFile(unit, original, 0600)
	if out, err := cliProcess(t, root, env, "service", "install").CombinedOutput(); err == nil || !strings.Contains(string(out), "unmanaged") {
		t.Fatalf("unmanaged service not protected: %s %v", out, err)
	}
	if out, err := cliProcess(t, root, env, "service", "install", "--replace", "--start").CombinedOutput(); err != nil {
		t.Fatalf("install: %s %v", out, err)
	}
	backup, _ := os.ReadFile(unit + ".bak")
	if string(backup) != string(original) {
		t.Fatal("original service not preserved")
	}
	config := filepath.Join(root, "config", "openslot", "config.env")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := cliProcess(t, root, env, "service", "uninstall").CombinedOutput(); err != nil {
		t.Fatalf("uninstall: %s %v", out, err)
	}
	after, _ := os.ReadFile(config)
	if string(before) != string(after) {
		t.Fatal("uninstall changed configuration")
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatal("managed unit remains")
	}
	calls, _ := os.ReadFile(log)
	for _, call := range []string{"--user daemon-reload", "--user enable openslot.service", "--user restart openslot.service", "--user disable --now openslot.service"} {
		if !strings.Contains(string(calls), call) {
			t.Fatalf("missing %s", call)
		}
	}
}
