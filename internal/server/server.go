package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"weatherhds/internal/music"
	"weatherhds/internal/vocallocal"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func Run(ctx context.Context, embeddedWebroot fs.FS) error {
	logger := bootLogger()
	workspaceDir, err := os.Getwd()
	if err != nil {
		return err
	}
	runtimePaths, err := ensureRuntimeFiles(workspaceDir, embeddedWebroot)
	if err != nil {
		return err
	}
	_ = godotenv.Load(runtimePaths.EnvPath)
	twcAPIKey := strings.TrimSpace(os.Getenv("TWC_API_KEY"))
	cfg := loadConfig(runtimePaths.ConfigPath)
	publicDir := runtimePaths.PublicDir
	versionID := loadVersionFromConfig(runtimePaths.ConfigPath)
	printStartupArt()
	logInfo(logger, "server", "==========================================================================================================")
	logInfo(logger, "server", "WeatherHDS Server v%s", versionID)
	logInfo(logger, "server", "Created by raiii. (c) SSPWXR/raii 2025")
	logInfo(logger, "server", "User contributors: ScentedOrangeDev, LeWolfYt,")
	localeCache, err := NewPersistentLocaleCache(workspaceDir)
	if err != nil {
		return err
	}
	ttlCache := NewTTLCache(time.Duration(cfg.CacheValidTime) * time.Second)
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	weather, err := newProvider(cfg.DataProvider, twcAPIKey, cfg.Units, client, localeCache, ttlCache, logger)
	if err != nil {
		return err
	}
	backgrounds := NewBackgroundManager(publicDir)
	vocalLocalService, err := vocallocal.NewService(publicDir)
	if err != nil {
		return err
	}
	loudness, normalize, err := music.ParseStandard(cfg.MusicLoudness)
	if err != nil {
		return err
	}
	var musicOptions []music.Option
	if normalize {
		musicOptions = append(musicOptions, music.WithLoudness(loudness, filepath.Join(workspaceDir, "persistentCache", "music-loudness.json")))
	}
	musicService, err := music.NewService(filepath.Join(workspaceDir, "Music"), func(format string, args ...any) {
		logInfo(logger, "music", format, args...)
	}, musicOptions...)
	if err != nil {
		return err
	}
	defer musicService.Close()
	musicService.Start(ctx)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(staticCacheHeaders)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			status := c.Response().Status
			if status == 0 {
				status = http.StatusOK
			}
			elapsed := time.Since(start)
			isLongLived := c.Request().URL.Path == "/music/ws"
			isError := status >= http.StatusInternalServerError || (err != nil && status >= http.StatusBadRequest)
			if isError || (!isLongLived && elapsed >= 500*time.Millisecond) {
				logInfo(logger, "server/http", "HTTP %s %s %d %s", c.Request().Method, c.Request().URL.Path, status, elapsed.Round(time.Millisecond))
			}
			return err
		}
	})

	e.GET("/config/keys", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"twcApiKey": twcAPIKey})
	})
	e.GET("/heartbeat", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "timestamp": time.Now().Format(time.RFC3339)})
	})
	e.HEAD("/heartbeat", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.NoContent(http.StatusOK)
	})
	e.GET("/data", func(c echo.Context) error {
		return c.String(http.StatusOK, "ARE YOU HAVE STUPID??? YOU ARE SUPOSED TO AD PARAMTER LIKE /data/MEMPHOS?loctype=primary!!!!!")
	})
	e.GET("/data/alerts/:location", func(c echo.Context) error {
		location, err := locationParam(c)
		if err != nil {
			return c.String(http.StatusBadRequest, err.Error())
		}
		localeData, err := weather.LoadLocaleData(c.Request().Context(), location)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Error fetching alert data"})
		}
		alertData, statusCode, err := weather.FetchAlerts(c.Request().Context(), localeData)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Error fetching alert data"})
		}
		if statusCode == http.StatusNoContent {
			return c.String(http.StatusNoContent, "No active alerts for the requested location.")
		}
		return c.JSON(http.StatusOK, alertData)
	})
	e.GET("/data/:location", func(c echo.Context) error {
		location, err := locationParam(c)
		if err != nil {
			return c.String(http.StatusBadRequest, err.Error())
		}
		locType := c.QueryParam("locType")
		requestContext := c.Request().Context()
		if strings.Contains(location, "|") || strings.Contains(location, ";") {
			delimiter := "|"
			if strings.Contains(location, ";") {
				delimiter = ";"
			}
			parts := strings.Split(location, delimiter)
			combined := make([]map[string]any, 0, len(parts))
			for _, part := range parts {
				localeData, err := weather.LoadLocaleData(requestContext, strings.TrimSpace(part))
				if err != nil {
					return c.String(http.StatusInternalServerError, err.Error())
				}
				resolvedLocType := locType
				if resolvedLocType == "" {
					resolvedLocType = "primary"
				}
				wxData, err := weather.LoadWeatherData(requestContext, localeData, resolvedLocType)
				if err != nil {
					return c.String(http.StatusInternalServerError, err.Error())
				}
				combined = append(combined, map[string]any{"localeData": localeData, "wxData": wxData})
			}
			return c.JSON(http.StatusOK, combined)
		}
		localeData, err := weather.LoadLocaleData(requestContext, location)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		if locType == "" {
			return c.JSON(http.StatusBadRequest, map[string]any{"error": true, "comment": "Please add a locType query"})
		}
		wxData, err := weather.LoadWeatherData(requestContext, localeData, locType)
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]any{
			"metadata": map[string]any{"localeData": localeData, "units": cfg.Units, "hdsLocType": locType},
			"weather":  wxData,
		})
	})
	e.POST("/backgrounds/init", func(c echo.Context) error {
		payload, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid body")
		}
		wxCond := "wxgood"
		body := strings.TrimSpace(string(payload))
		if len(body) >= 2 && strings.HasPrefix(body, "[") && strings.HasSuffix(body, "]") {
			wxCond = body[1 : len(body)-1]
		}
		initialized := backgrounds.InitializedCondition()
		if initialized != "" && initialized == wxCond {
			return c.String(http.StatusOK, "Backgrounds are already initialized with weather condition: "+initialized)
		}
		if ok := backgrounds.Init(wxCond); !ok {
			return c.String(http.StatusNotFound, "No valid backgrounds found for weather condition: "+wxCond)
		}
		return c.String(http.StatusOK, "Initialized/re-initialized local backgrounds with weather condition: "+wxCond)
	})
	e.GET("/backgrounds/image", func(c echo.Context) error {
		selected := backgrounds.CurrentImage()
		if selected == "" {
			return c.String(http.StatusNoContent, "Background image not initialized.")
		}
		absolutePath := filepath.Join(publicDir, filepath.FromSlash(strings.TrimPrefix(selected, "/")))
		if !isPathInPublicDir(publicDir, absolutePath) {
			return c.String(http.StatusBadRequest, "Invalid background image path.")
		}
		if !fileExists(absolutePath) {
			return c.String(http.StatusNotFound, "No valid background image files available for current condition.")
		}
		return c.File(absolutePath)
	})
	e.GET("/bing-background", func(c echo.Context) error {
		request, err := http.NewRequestWithContext(c.Request().Context(), http.MethodGet, "https://www.bing.com/HPImageArchive.aspx?format=js&idx=0&n=1&mkt=en-US", nil)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		response, err := client.Do(request)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		defer response.Body.Close()
		var payload any
		if err := jsonDecode(response, &payload); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		logInfo(logger, "server/api", "Client requested Bing background image")
		return c.JSON(http.StatusOK, payload)
	})
	e.POST("/vocallocal/clips", func(c echo.Context) error {
		request := vocallocal.ClipRequest{}
		if err := c.Bind(&request); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}
		response, err := vocalLocalService.EnsureClips(c.Request().Context(), request)
		if err != nil {
			message := err.Error()
			if strings.Contains(message, "text is empty") || strings.Contains(message, "unknown section") || strings.Contains(message, "unknown phrase key") {
				return c.JSON(http.StatusBadRequest, map[string]string{"error": message})
			}
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": message})
		}
		return c.JSON(http.StatusOK, response)
	})
	e.GET("/music/ws", func(c echo.Context) error {
		musicService.HandleWebSocket(c.Response(), c.Request())
		return nil
	})
	e.GET("/music/state", func(c echo.Context) error {
		musicService.HandleState(c.Response(), c.Request())
		return nil
	})
	e.GET("/music/art/:trackID", func(c echo.Context) error {
		musicService.HandleArt(c.Response(), c.Request(), c.Param("trackID"))
		return nil
	})
	e.File("/", filepath.Join(publicDir, "index.html"))
	e.Static("/persistentCache", filepath.Join(workspaceDir, "persistentCache"))
	e.Static("/", publicDir)

	port := cfg.WebPort
	for {
		listener, listenErr := net.Listen("tcp", ":"+strconv.Itoa(port))
		if listenErr != nil {
			if isAddrInUse(listenErr) {
				logInfo(logger, "server", "Port %d is already in use, trying %d", port, port+1)
				port++
				continue
			}
			return listenErr
		}
		logInfo(logger, "server", "HTTP server listening on http://localhost:%d", port)
		httpServer := &http.Server{Handler: e, ReadHeaderTimeout: 10 * time.Second}
		serverCtx, stopServers := context.WithCancel(ctx)
		defer stopServers()
		go func() {
			<-serverCtx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
		}()

		// The admin panel is optional: if its port can't be bound (ports below 1024 need privileges on Linux),
		// log it and keep serving the display instead of exiting.
		if adminListener, adminErr := net.Listen("tcp", ":"+strconv.Itoa(adminPort)); adminErr != nil {
			logInfo(logger, "server", "Admin panel disabled, cannot listen on port %d: %v", adminPort, adminErr)
		} else {
			adminServer := &http.Server{Handler: newAdminHandler(runtimePaths.AdminDir), ReadHeaderTimeout: 10 * time.Second}
			go func() {
				<-serverCtx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = adminServer.Shutdown(shutdownCtx)
			}()
			go func() {
				logInfo(logger, "server", "Admin panel listening on http://localhost:%d", adminPort)
				if serveErr := adminServer.Serve(adminListener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					logInfo(logger, "server", "Admin panel server stopped: %v", serveErr)
				}
			}()
		}
		return httpServer.Serve(listener)
	}
}

func loadConfig(configPath string) AppConfig {
	cfg := AppConfig{Units: "m", WebPort: 3000, CacheValidTime: 720}
	payload, err := os.ReadFile(configPath)
	if err == nil {
		serverConfig := extractJSObjectAssignment(string(payload), "serverConfig")
		if value := extractJSString(serverConfig, "units"); value != "" {
			cfg.Units = value
		}
		if value := extractJSInt(serverConfig, "webPort"); value > 0 {
			cfg.WebPort = value
		}
		if value := extractJSInt(serverConfig, "cacheValidTime"); value > 0 {
			cfg.CacheValidTime = value
		}
		cfg.DataProvider = extractJSString(serverConfig, "dataProvider")
		cfg.MusicLoudness = extractJSString(serverConfig, "musicLoudness")
	}
	if value := strings.TrimSpace(os.Getenv("UNITS")); value != "" {
		cfg.Units = value
	}
	if value := strings.TrimSpace(os.Getenv("DATA_PROVIDER")); value != "" {
		cfg.DataProvider = value
	}
	if value := strings.TrimSpace(os.Getenv("MUSIC_LOUDNESS")); value != "" {
		cfg.MusicLoudness = value
	}
	if value := strings.TrimSpace(os.Getenv("WEB_PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.WebPort = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("CACHE_VALID_TIME")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.CacheValidTime = parsed
		}
	}
	if value := strings.TrimSpace(os.Getenv("PORT")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			cfg.WebPort = parsed
		}
	}
	return cfg
}

func extractJSObjectAssignment(source string, name string) string {
	pattern := regexp.MustCompile(`(?s)\b` + regexp.QuoteMeta(name) + `\s*=\s*\{(.*?)\n\}`)
	matches := pattern.FindStringSubmatch(source)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func extractJSString(source string, key string) string {
	pattern := regexp.MustCompile(`["']` + regexp.QuoteMeta(key) + `["']\s*:\s*["']([^"']+)["']`)
	matches := pattern.FindStringSubmatch(source)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func extractJSInt(source string, key string) int {
	pattern := regexp.MustCompile(`["']` + regexp.QuoteMeta(key) + `["']\s*:\s*([0-9]+)`)
	matches := pattern.FindStringSubmatch(source)
	if len(matches) < 2 {
		return 0
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return value
}

func staticCacheHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		path := c.Request().URL.Path
		if isCacheableStaticPath(path) {
			c.Response().Header().Set("Cache-Control", "public, max-age=3600")
		}
		return next(c)
	}
}

func isCacheableStaticPath(path string) bool {
	if path == "/" || path == "/config.js" || path == "/heartbeat" {
		return false
	}
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".avif", ".webp", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".css", ".js", ".ogg", ".wav", ".json":
		return true
	default:
		return false
	}
}

// locationParam returns the :location route param decoded. Echo routes on the raw path once it holds
// escapes (encodeURIComponent turns "Saskatoon, SK" into "Saskatoon%2C%20SK"), so params arrive encoded.
func locationParam(c echo.Context) (string, error) {
	location, err := url.PathUnescape(c.Param("location"))
	if err != nil {
		return "", fmt.Errorf("invalid location: %w", err)
	}
	return location, nil
}

func jsonDecode(response *http.Response, target any) error {
	return json.NewDecoder(response.Body).Decode(target)
}

func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "address already in use") || strings.Contains(message, "only one usage of each socket address")
}
