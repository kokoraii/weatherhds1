package server

type AppConfig struct {
	Units          string
	WebPort        int
	AdminPort      int    // admin panel and command API; ADMIN_PORT overrides
	SSHPort        int    // the SSH console; SSH_PORT overrides
	SystemTimeZone string // config.systemTimeZone: the station's local time
	CacheValidTime int
	DataProvider   string
	MusicLoudness  string
}
