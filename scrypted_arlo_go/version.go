package scrypted_arlo_go

import (
	_ "embed"
	"fmt"
	"strconv"
	"time"
)

//go:generate bash -c "printf %s $(git rev-parse HEAD) > VERSION.txt"
//go:embed VERSION.txt
var version string

//go:generate bash -c "printf %s $(date +%s) > BUILDTIME.txt"
//go:embed BUILDTIME.txt
var buildTime string

var parsedBuildTime time.Time

func init() {
	t, err := strconv.ParseInt(buildTime, 10, 64)
	if err != nil {
		panic(fmt.Errorf("could not parse BuildTime: %w", err))
	}
	parsedBuildTime = time.Unix(t, 0)
}
