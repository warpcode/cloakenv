package cmd

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

var (
	// Version holds the current release version of cloakenv.
	// Can be overridden at build time via -ldflags="-X github.com/warpcode/cloakenv/internal/cmd.Version=...".
	Version = "dev"

	// Commit holds the git commit SHA at build time.
	// Can be overridden via -ldflags="-X github.com/warpcode/cloakenv/internal/cmd.Commit=...".
	Commit = "unknown"

	// BuildDate holds the build timestamp.
	// Can be overridden via -ldflags="-X github.com/warpcode/cloakenv/internal/cmd.BuildDate=...".
	BuildDate = "unknown"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if Commit == "unknown" && setting.Value != "" {
					Commit = setting.Value
				}
			case "vcs.time":
				if BuildDate == "unknown" && setting.Value != "" {
					BuildDate = setting.Value
				}
			}
		}
	}
}

// VersionInfo holds metadata describing the build.
type VersionInfo struct {
	Version   string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	BuildDate string `json:"build_date" yaml:"build_date"`
	GoVersion string `json:"go_version" yaml:"go_version"`
	Platform  string `json:"platform" yaml:"platform"`
}

// GetVersionInfo returns structured version information.
func GetVersionInfo() VersionInfo {
	return VersionInfo{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// PrintVersion writes human-readable version info to the provided writer.
func PrintVersion(w io.Writer) {
	info := GetVersionInfo()
	fmt.Fprintf(w, "cloakenv version %s (commit: %s, built: %s, runtime: %s, %s)\n",
		info.Version, info.Commit, info.BuildDate, info.GoVersion, info.Platform)
}

// VersionCmd handles "cloakenv version" subcommand and prints build metadata.
func VersionCmd(args []string, w io.Writer) int {
	PrintVersion(w)
	return 0
}
