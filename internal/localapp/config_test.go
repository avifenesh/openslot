package localapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

func TestInitPersistsPrivateKeysAndQuotedStorage(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings", "config.env")
	data := filepath.Join(root, "storage with $dollar and spaces")
	created, e := Init(path, data)
	if e != nil || !created {
		t.Fatal(created, e)
	}
	first, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	values, e := godotenv.Read(path)
	if e != nil {
		t.Fatal(e)
	}
	if values["DATA_DIR"] != data || values["DATABASE_URL"] != "sqlite://"+filepath.Join(data, "openslot.db") {
		t.Fatal("storage paths did not round trip")
	}
	if len(values["CALNODE_ENCRYPTION_KEY"]) != 64 || len(values["CALNODE_RECOVERY_SECRET"]) != 64 || values["CALNODE_ENCRYPTION_KEY"] == values["CALNODE_RECOVERY_SECRET"] {
		t.Fatal("missing independent persistent secrets")
	}
	if values["BIND_HOST"] != "127.0.0.1" || values["PORT"] != "8788" {
		t.Fatal("wrong safe first-run address")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("configuration is not private")
	}
	created, e = Init(path, data)
	if e != nil || created {
		t.Fatal(created, e)
	}
	again, _ := os.ReadFile(path)
	if string(first) != string(again) {
		t.Fatal("existing secrets were replaced")
	}
}
func TestInitRefusesOrphanedData(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	os.Mkdir(data, 0700)
	os.WriteFile(filepath.Join(data, "openslot.db"), []byte("existing database"), 0600)
	if _, e := Init(filepath.Join(root, "new.env"), data); e == nil {
		t.Fatal("must not create new keys for existing data")
	}
}
func TestLoadAnchorsFilePathsButHonorsEnvironment(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.env")
	for _, key := range []string{"DATABASE_URL", "DATA_DIR", "BASE_URL"} {
		value, exists := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if exists {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		})
	}
	if e := os.WriteFile(path, []byte("DATABASE_URL=sqlite://./data/old.db\nDATA_DIR=./uploads\nBASE_URL=http://localhost:8788\n"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("BASE_URL", "https://schedule.example.test")
	if e := Load(path); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("DATABASE_URL") != "sqlite://"+filepath.Join(root, "data/old.db") || os.Getenv("DATA_DIR") != filepath.Join(root, "uploads") {
		t.Fatal("config-relative paths not anchored")
	}
	if os.Getenv("BASE_URL") != "https://schedule.example.test" {
		t.Fatal("environment override was replaced")
	}
}
func TestResolveExplicitAndEnvironmentOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("OPENSLOT_CONFIG", "")
	t.Setenv("DATABASE_URL", "sqlite://./existing.db")
	p, e := Resolve("")
	if e != nil || p != "" {
		t.Fatal("environment-only deployment changed", p, e)
	}
	p, e = Resolve("specific.env")
	if e != nil || !filepath.IsAbs(p) || !strings.HasSuffix(p, "specific.env") {
		t.Fatal(p, e)
	}
}

func TestLoadAnchorsRelativeSQLiteURIThroughSymlink(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "real", "config.env")
	os.MkdirAll(filepath.Dir(original), 0700)
	link := filepath.Join(root, "linked.env")
	if err := os.Symlink(original, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ input, want string }{
		{"file:openslot%20data.db?cache=shared", "file://" + filepath.Join(root, "real", "openslot%20data.db") + "?cache=shared"},
		{"file::memory:?cache=shared", "file::memory:?cache=shared"},
		{"file:memdb?mode=memory&cache=shared", "file:memdb?mode=memory&cache=shared"},
		{"file:///absolute.db?cache=shared", "file:///absolute.db?cache=shared"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "")
			os.Unsetenv("DATABASE_URL")
			if err := os.WriteFile(original, []byte("DATABASE_URL="+tc.input+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := Load(link); err != nil {
				t.Fatal(err)
			}
			if got := os.Getenv("DATABASE_URL"); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
