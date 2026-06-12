package version

import (
	"fmt"
	"strings"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return fmt.Sprintf("version=%s commit=%s build_date=%s", Version, Commit, BuildDate)
}

func Info(name string) string {
	return fmt.Sprintf("%s %s", name, String())
}

func ShouldPrint(args []string) bool {
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "version", "--version", "-version":
			return true
		}
	}
	return false
}

func EffectiveVersion(fallback string) string {
	if strings.TrimSpace(Version) != "" && Version != "dev" {
		return Version
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return Version
}
