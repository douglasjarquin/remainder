package main

import (
	"debug/buildinfo"
	"fmt"
)

func inspectBuildPortable(binary string) buildMetadata {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return buildMetadata{Status: "unknown", UnavailableReason: err.Error()}
	}
	result := buildMetadata{
		Status:         "observed",
		GoVersion:      info.GoVersion,
		Path:           info.Path,
		SourceRevision: "unknown",
		SourceModified: "unknown",
		BuildSettings:  make(map[string]string, len(info.Settings)),
	}
	for _, dependency := range info.Deps {
		result.Dependencies = append(result.Dependencies, fmt.Sprintf("%s %s", dependency.Path, dependency.Version))
	}
	for _, setting := range info.Settings {
		result.BuildSettings[setting.Key] = setting.Value
		switch setting.Key {
		case "vcs.revision":
			result.SourceRevision = setting.Value
		case "vcs.modified":
			result.SourceModified = setting.Value
		}
	}
	return result
}
