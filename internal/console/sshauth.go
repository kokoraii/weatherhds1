package console

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

// Authorizer decides who may open the console over SSH the way the machine's own sshd would: the name
// must be a system user, and the key must be in an AuthorizedKeysFile that sshd_config names for them.
// The config is read on every login, so edits apply at once. Only public keys are accepted.
//
// Where the config can't be read (many distributions keep it root-only, and the server usually runs as
// an ordinary user), sshd's defaults apply: ~/.ssh/authorized_keys. A server running as one user can
// only read that user's own key files anyway, so in practice that user's keys are the ones let in.
type Authorizer struct {
	ConfigPath string
}

// account is what the login rules need to know about a user.
type account struct {
	name, home, uid string
	groups          []string
}

// Allows reports whether this user may log in with this key from this address, and if not, why.
func (a *Authorizer) Allows(username, remoteIP string, key gossh.PublicKey) (bool, string) {
	acct, err := lookupAccount(username)
	if err != nil {
		return false, "no such user"
	}
	config, err := readSSHDConfig(a.ConfigPath)
	if err != nil && !unreadable(err) {
		return false, "can't read " + a.ConfigPath + ": " + err.Error()
	}
	settings := config.settingsFor(acct, remoteIP)
	if reason := settings.refuses(acct); reason != "" {
		return false, reason
	}
	for _, file := range settings.authorizedKeysFiles(acct) {
		if keyFileHas(file, key) {
			return true, ""
		}
	}
	return false, "key not in the authorized keys"
}

func lookupAccount(username string) (account, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return account{}, err
	}
	acct := account{name: username, home: u.HomeDir, uid: u.Uid}
	ids, _ := u.GroupIds()
	for _, id := range ids {
		if group, err := user.LookupGroupId(id); err == nil {
			acct.groups = append(acct.groups, group.Name)
		}
	}
	return acct, nil
}

// unreadable is a config file that's missing or off limits to this process: sshd's defaults apply.
func unreadable(err error) bool {
	return os.IsNotExist(err) || os.IsPermission(err)
}

// directive is one line of sshd_config: a keyword (lowercased) and its arguments.
type directive struct {
	keyword string
	args    []string
}

// sshdConfig is sshd_config as written, with Includes expanded in place.
type sshdConfig struct {
	directives []directive
}

func readSSHDConfig(configPath string) (sshdConfig, error) {
	var config sshdConfig
	err := config.read(configPath, filepath.Dir(configPath), 0)
	return config, err
}

func (c *sshdConfig) read(file, baseDir string, depth int) error {
	if depth > 8 {
		return fmt.Errorf("Include nested too deeply at %s", file)
	}
	payload, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	for scanner.Scan() {
		fields := configFields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		d := directive{keyword: strings.ToLower(fields[0]), args: fields[1:]}
		if d.keyword != "include" {
			c.directives = append(c.directives, d)
			continue
		}
		for _, pattern := range d.args {
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(baseDir, pattern)
			}
			matches, _ := filepath.Glob(pattern)
			for _, match := range matches {
				if err := c.read(match, baseDir, depth+1); err != nil && !unreadable(err) {
					return err
				}
			}
		}
	}
	return scanner.Err()
}

// configFields splits a config line into words, dropping comments and allowing "keyword=value".
func configFields(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	if keyword, rest, found := strings.Cut(line, "="); found && !strings.ContainsAny(keyword, " \t") {
		line = keyword + " " + rest
	}
	words, _ := split(strings.ReplaceAll(line, "\t", " "))
	return words
}

// settings are the values that apply to one login. As in sshd: within the global section, and within
// the Match blocks that match, the first value of a keyword wins; a matching Match block overrides the
// global section; and the allow and deny lists add up instead.
type settings map[string][]string

var accumulating = map[string]bool{"allowusers": true, "denyusers": true, "allowgroups": true, "denygroups": true}

func (c sshdConfig) settingsFor(acct account, remoteIP string) settings {
	global, matched := settings{}, settings{}
	section := global // until the first Match
	for _, d := range c.directives {
		if d.keyword == "match" {
			section = nil
			if matchBlock(d.args, acct, remoteIP) {
				section = matched
			}
			continue
		}
		if section == nil {
			continue
		}
		if accumulating[d.keyword] {
			section[d.keyword] = append(section[d.keyword], d.args...)
		} else if _, set := section[d.keyword]; !set {
			section[d.keyword] = d.args
		}
	}
	for keyword, args := range matched {
		if accumulating[keyword] {
			global[keyword] = append(global[keyword], args...)
		} else {
			global[keyword] = args
		}
	}
	return global
}

// matchBlock evaluates a Match line. Criteria it can't evaluate here (Host, LocalPort, ...) don't match.
func matchBlock(args []string, acct account, remoteIP string) bool {
	if len(args) == 1 && strings.EqualFold(args[0], "all") {
		return true
	}
	if len(args)%2 != 0 {
		return false
	}
	for i := 0; i < len(args); i += 2 {
		criterion, value := strings.ToLower(args[i]), args[i+1]
		switch criterion {
		case "user":
			if !matchList(acct.name, value) {
				return false
			}
		case "group":
			if !anyMatch(acct.groups, value) {
				return false
			}
		case "address":
			if !matchAddress(remoteIP, value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// refuses says why these settings shut the user out, or "" if they don't.
func (s settings) refuses(acct account) string {
	if first(s["pubkeyauthentication"]) == "no" {
		return "PubkeyAuthentication is off"
	}
	if acct.name == "root" && first(s["permitrootlogin"]) == "no" {
		return "PermitRootLogin is no"
	}
	if deny, set := s["denyusers"]; set && anyUserMatch(acct.name, deny) {
		return "listed in DenyUsers"
	}
	if allow, set := s["allowusers"]; set && !anyUserMatch(acct.name, allow) {
		return "not listed in AllowUsers"
	}
	if deny, set := s["denygroups"]; set && anyMatch(acct.groups, strings.Join(deny, ",")) {
		return "in a group listed in DenyGroups"
	}
	if allow, set := s["allowgroups"]; set && !anyMatch(acct.groups, strings.Join(allow, ",")) {
		return "in no group listed in AllowGroups"
	}
	return ""
}

// authorizedKeysFiles lists the user's key files, with sshd's tokens expanded and relative paths taken
// from their home directory.
func (s settings) authorizedKeysFiles(acct account) []string {
	files, set := s["authorizedkeysfile"]
	if !set {
		files = []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}
	}
	var out []string
	for _, file := range files {
		if strings.EqualFold(file, "none") {
			continue
		}
		file = expandTokens(file, acct)
		if !filepath.IsAbs(file) && !strings.HasPrefix(file, "/") {
			file = filepath.Join(acct.home, file)
		}
		out = append(out, file)
	}
	return out
}

func expandTokens(file string, acct account) string {
	file = strings.ReplaceAll(file, "%%", "\x00")
	file = strings.ReplaceAll(file, "%h", acct.home)
	file = strings.ReplaceAll(file, "%u", acct.name)
	file = strings.ReplaceAll(file, "%U", acct.uid)
	file = strings.ReplaceAll(file, "__PROGRAMDATA__", os.Getenv("ProgramData")) // Windows OpenSSH
	return strings.ReplaceAll(file, "\x00", "%")
}

// keyFileHas reports whether an authorized_keys file lists this key. Keys with options (command=,
// from=, restrict, ...) are skipped: honouring them is sshd's job, and ignoring them would grant more
// than the owner meant.
func keyFileHas(file string, key gossh.PublicKey) bool {
	payload, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	wanted := key.Marshal()
	for len(payload) > 0 {
		listed, _, options, rest, err := gossh.ParseAuthorizedKey(payload)
		if err != nil {
			return false
		}
		payload = rest
		if len(options) == 0 && bytes.Equal(listed.Marshal(), wanted) {
			return true
		}
	}
	return false
}

// matchList matches a value against an sshd pattern list ("a*,b?,!c"): no negated pattern may match and
// at least one plain one must. Windows names are matched without case, as Windows treats them.
func matchList(value, list string) bool {
	matched := false
	for _, pattern := range strings.Split(list, ",") {
		negated := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		if !globMatch(pattern, value) {
			continue
		}
		if negated {
			return false
		}
		matched = true
	}
	return matched
}

func anyMatch(values []string, list string) bool {
	for _, value := range values {
		if matchList(value, list) {
			return true
		}
	}
	return false
}

// anyUserMatch checks AllowUsers/DenyUsers entries, which may be USER@HOST; the host part isn't checked.
func anyUserMatch(name string, entries []string) bool {
	for _, entry := range entries {
		pattern, _, _ := strings.Cut(entry, "@")
		if globMatch(pattern, name) {
			return true
		}
	}
	return false
}

func globMatch(pattern, value string) bool {
	if runtime.GOOS == "windows" {
		pattern, value = strings.ToLower(pattern), strings.ToLower(value)
	}
	ok, err := path.Match(pattern, value)
	return err == nil && ok
}

// matchAddress matches an IP against a list of addresses, wildcards and CIDR ranges.
func matchAddress(ip, list string) bool {
	parsed := net.ParseIP(ip)
	matched := false
	for _, pattern := range strings.Split(list, ",") {
		negated := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		hit := globMatch(pattern, ip)
		if _, network, err := net.ParseCIDR(pattern); err == nil && parsed != nil {
			hit = network.Contains(parsed)
		}
		if !hit {
			continue
		}
		if negated {
			return false
		}
		matched = true
	}
	return matched
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.ToLower(values[0])
}
