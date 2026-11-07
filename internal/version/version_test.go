package version

import (
	"os"
	"regexp"
	"testing"
)

// Keep the alternate CI Dockerfile's default version consistent with the
// dashboard/GraphQL/health version. Release builds may override both at build time.
func TestCIDockerfileVersionMatchesReportedVersion(t *testing.T) {
	content, err := os.ReadFile("../../ci.Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	versionArg := regexp.MustCompile(`(?m)^ARG VERSION=(v[0-9]+\\.[0-9]+\\.[0-9]+)$`)
	matches := versionArg.FindStringSubmatch(string(content))
	if len(matches) != 2 {
		t.Fatal("ci.Dockerfile must declare a semver ARG VERSION default")
	}
	if matches[1] != GitTag {
		t.Errorf("ci.Dockerfile defaults to %q, dashboard reports %q", matches[1], GitTag)
	}
}
