package version

import (
	"context"

	"github.com/google/go-github/github"
)

const versionUnknown = "v0.0.0-unknown"

var Version string = versionUnknown

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
	if Version != versionUnknown {
		return Version
	}

	tag, err := getLatestReleaseTag(ctx)
	if err != nil {
		return Version
	}

	return tag
}
