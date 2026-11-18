package server

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/lmittmann/tint"
)

const startupArtBase64 = "ICAgICAgICAgICAgICAgICAgICDilojilogKICAgICAgICAgICDilojiloggICAgICAg4paI4paIICAgICDilojilogKICAgICAgICAgICAg4paI4paI4paIICAgICAgICAgIOKWiOKWiOKWiCAgICAgICAgICAgICAgICAgIOKWiOKWiOKWiOKWiCAgICAgICAg4paI4paI4paIICDilojilojilojilojilojilojilojilojilojiloggICAgICAgICDilojilojilojilojilojilojilojilogKICAgICAgICAgIOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiCAgICAgICAgICAgICAgIOKWiOKWiCAgICDilojilojilojiloggICAgICAgIOKWiOKWiOKWiCAg4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paIICAgIOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiCAgICDilojilojilogKICAgICAgICDilojilojilojilojilogg4paI4paI4paI4paI4paIICAgIOKWiOKWiCAgICAgICAgICAgIOKWiOKWiOKWiOKWiOKWiOKWiCAgIOKWiOKWiOKWiOKWiCAgICAgICAg4paI4paI4paIICDilojilojilojiloggICAgIOKWiOKWiOKWiOKWiOKWiOKWiCDilojilojilojilojilojiloggICAg4paI4paI4paI4paIICAg4paI4paI4paI4paI4paI4paICiAgICAg4paI4paI4paI4paI4paIICAgICAg4paI4paI4paI4paI4paI4paI4paI4paI4paIICAg4paI4paI4paIICAg4paI4paI4paI4paI4paI4paI4paIICAgICDilojilojilojilojilojilojilojilojilojilojilojilojilojilojiloggIOKWiOKWiOKWiOKWiCAgICAgIOKWiOKWiOKWiOKWiOKWiOKWiCDilojilojilojilojilojilojilojilojiloggICAgICAgICDilojilojilojilojilojilojilogKICAg4paI4paI4paI4paI4paI4paI4paIICAgICAgICDilojilojilojilojilojilojiloggICAgICAgIOKWiOKWiOKWiOKWiOKWiCAgICAgICAg4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paIICDilojilojilojiloggICAgICAg4paI4paI4paI4paI4paIICDilojilojilojilojilojilojilojilojilojilojilojiloggICAgICAg4paI4paI4paI4paI4paI4paICiAgIOKWiOKWiCAg4paIICAgICAgICAgICAgICAg4paI4paIICAgICAgICAg4paI4paI4paI4paI4paI4paIICAgICAg4paI4paI4paI4paIICAgICAgICDilojilojiloggIOKWiOKWiOKWiOKWiCAgICAgIOKWiOKWiOKWiOKWiOKWiCAgICAgICAgIOKWiOKWiOKWiOKWiOKWiOKWiOKWiCAgICDilojilojilojilojilojilojilogKICDilojiloggICAg4paI4paIICAgICAgICAgICAgIOKWiOKWiCAgICAgICAgICAg4paI4paI4paI4paI4paI4paIICAgIOKWiOKWiOKWiOKWiCAgICAgICAg4paI4paI4paIICDilojilojilojiloggICAgICDilojilojilojilojilogg4paI4paI4paI4paI4paIICAgICAg4paI4paI4paI4paIICAg4paI4paI4paI4paI4paI4paICiDilojilojiloggICAg4paI4paI4paIIOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiCAgICAgICAgICAgICAg4paI4paI4paI4paIICAg4paI4paI4paI4paIICAgICAgICDilojilojiloggIOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiCAgIOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiCAg4paI4paI4paI4paI4paICiAgIOKWiOKWiCAg4paI4paIICAg4paI4paI4paI4paI4paI4paI4paI4paI4paI4paI4paIICAgICAgICAgICAgICAgICAgICAgICDilojilojilojiloggICAgICAgIOKWiOKWiOKWiCAgIOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiOKWiCAgICAgICDilojilojilojilojilojilojilojilojilojilojilog="

// bootLogger writes the server log to w: the console's broker, which shows it on every console screen.
func bootLogger(w io.Writer) *slog.Logger {
	handler := tint.NewHandler(w, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: "2006-01-02 3:04:05 PM",
	})
	return slog.New(handler)
}

func logPrefix(module string) string {
	resolvedModule := strings.TrimSpace(module)
	if resolvedModule == "" {
		resolvedModule = callerModuleName()
	}
	return fmt.Sprintf("[%s]", resolvedModule)
}

func logInfo(logger *slog.Logger, module string, format string, args ...any) {
	logger.Info(fmt.Sprintf("%s %s", logPrefix(module), fmt.Sprintf(format, args...)))
}

func callerModuleName() string {
	_, file, _, ok := runtime.Caller(3)
	if !ok {
		return "server"
	}
	base := filepath.Base(file)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" {
		return "server"
	}
	return name
}

// printStartupArt clears the terminal, then writes the logo into the log (w), so console screens show it.
func printStartupArt(w io.Writer) {
	if decoded, err := base64.StdEncoding.DecodeString(startupArtBase64); err == nil {
		_, _ = fmt.Fprint(os.Stdout, "\x1Bc")
		_, _ = fmt.Fprintln(w, string(decoded))
	}
}

func loadVersionFromConfig(configPath string) string {
	payload, err := os.ReadFile(configPath)
	if err != nil {
		return "unknown"
	}
	re := regexp.MustCompile(`versionID\s*=\s*['\"]([^'\"]+)['\"]`)
	matches := re.FindStringSubmatch(string(payload))
	if len(matches) < 2 {
		return "unknown"
	}
	return strings.TrimSpace(matches[1])
}
