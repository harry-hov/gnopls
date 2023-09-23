package version

import (
	"context"
	"strings"

	"github.com/google/go-github/github"
)

const versionLocal = "local"

var Version = versionLocal

func getLatestReleaseTag(ctx context.Context) (string, error) {
	latest, _, err := github.
		NewClient(nil).
		Repositories.
		GetLatestRelease(ctx, "harry-hov", "gnopls")
	if err != nil {
		return "", err
	}

	if latest.TagName == nil {
		return "", nil
	}

	return *latest.TagName, nil
}

func GetVersion(ctx context.Context) string {
	if Version != versionLocal {
		return Version
	}

	tag, err := getLatestReleaseTag(ctx)
	if err != nil {
		return Version
	}

	if Version == versionLocal {
		parts := strings.Split(tag, "-")
		return parts[0] + "-" + versionLocal
	}

	return tag
}
