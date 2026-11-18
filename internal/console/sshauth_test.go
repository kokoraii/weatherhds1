package console

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func newKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := gossh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsFollowMatchBlocksAndIncludes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sshd_config"), strings.Join([]string{
		"# comments and blank lines are skipped",
		"",
		"Include sshd_config.d/*.conf",
		"AuthorizedKeysFile .ssh/authorized_keys",
		"AllowUsers rai admin@*",
		"Match Group administrators",
		"    AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys",
		"Match User guest",
		"    PubkeyAuthentication no",
	}, "\n"))
	writeFile(t, filepath.Join(dir, "sshd_config.d", "extra.conf"), "AllowUsers backup\n")

	config, err := readSSHDConfig(filepath.Join(dir, "sshd_config"))
	if err != nil {
		t.Fatal(err)
	}
	rai := account{name: "rai", home: "/home/rai", groups: []string{"users"}}
	settings := config.settingsFor(rai, "192.168.2.10")
	if got := settings.authorizedKeysFiles(rai); !slices.Equal(got, []string{filepath.Join("/home/rai", ".ssh/authorized_keys")}) {
		t.Fatalf("rai's key files: %q", got)
	}
	if reason := settings.refuses(rai); reason != "" {
		t.Fatalf("rai should be allowed, got %q", reason)
	}

	admin := account{name: "admin", home: "/home/admin", groups: []string{"administrators"}}
	adminFiles := config.settingsFor(admin, "10.0.0.1").authorizedKeysFiles(admin)
	if len(adminFiles) != 1 || !strings.HasSuffix(filepath.ToSlash(adminFiles[0]), "/ssh/administrators_authorized_keys") {
		t.Fatalf("a matching Match block overrides the global AuthorizedKeysFile, got %q", adminFiles)
	}

	stranger := account{name: "mallory", home: "/home/mallory"}
	if reason := config.settingsFor(stranger, "10.0.0.1").refuses(stranger); reason == "" {
		t.Fatal("someone not in AllowUsers should be refused")
	}
	backup := account{name: "backup", home: "/home/backup"}
	if reason := config.settingsFor(backup, "10.0.0.1").refuses(backup); reason != "" {
		t.Fatalf("AllowUsers lines add up, including the Included one; got %q", reason)
	}
	guest := account{name: "guest", home: "/home/guest"}
	if reason := config.settingsFor(guest, "10.0.0.1").refuses(guest); !strings.Contains(reason, "PubkeyAuthentication") {
		t.Fatalf("guest has public keys turned off, got %q", reason)
	}
}

func TestTokensExpand(t *testing.T) {
	acct := account{name: "rai", home: "/home/rai", uid: "1000"}
	if got := expandTokens("/keys/%u/%U/100%%", acct); got != "/keys/rai/1000/100%" {
		t.Fatalf("got %q", got)
	}
}

func TestKeyFileMatchesOnlyPlainListedKeys(t *testing.T) {
	dir := t.TempDir()
	listed, restricted, stranger := newKey(t), newKey(t), newKey(t)
	file := filepath.Join(dir, "authorized_keys")
	writeFile(t, file, string(gossh.MarshalAuthorizedKey(listed))+
		`command="/bin/false" `+string(gossh.MarshalAuthorizedKey(restricted)))
	if !keyFileHas(file, listed) {
		t.Fatal("a listed key should match")
	}
	if keyFileHas(file, restricted) {
		t.Fatal("a key with options should be skipped")
	}
	if keyFileHas(file, stranger) {
		t.Fatal("an unlisted key shouldn't match")
	}
	if keyFileHas(filepath.Join(dir, "missing"), listed) {
		t.Fatal("a missing file has no keys")
	}
}

func TestMatchListAndAddresses(t *testing.T) {
	if !matchList("rai", "r*,!root") || matchList("root", "r*,!root") {
		t.Fatal("pattern lists: plain patterns match, negated ones veto")
	}
	if !matchAddress("192.168.2.10", "192.168.2.0/24") || matchAddress("10.0.0.1", "192.168.2.0/24") {
		t.Fatal("CIDR addresses")
	}
	if !matchAddress("10.0.0.1", "10.0.0.*") || matchAddress("10.0.0.1", "10.*,!10.0.0.1") {
		t.Fatal("wildcard and negated addresses")
	}
}

func TestAMissingConfigMeansSSHDDefaults(t *testing.T) {
	config, err := readSSHDConfig(filepath.Join(t.TempDir(), "absent"))
	if err == nil || !unreadable(err) {
		t.Fatalf("expected an unreadable-config error, got %v", err)
	}
	acct := account{name: "rai", home: "/home/rai"}
	files := config.settingsFor(acct, "10.0.0.1").authorizedKeysFiles(acct)
	if len(files) != 2 || !strings.HasSuffix(filepath.ToSlash(files[0]), ".ssh/authorized_keys") {
		t.Fatalf("defaults should be ~/.ssh/authorized_keys and authorized_keys2, got %q", files)
	}
}
