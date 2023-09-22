package env

import (
	"fmt"
	"os"
	"path/filepath"
)

type Env struct {
	GNOROOT string
	GNOHOME string
}

func GnoHome() string {
	dir := os.Getenv("GNO_HOME")
	if dir != "" {
		return dir
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		panic(fmt.Errorf("couldn't get user config dir: %w", err))
	}
	gnoHome := filepath.Join(dir, "gno")
	return gnoHome
}
