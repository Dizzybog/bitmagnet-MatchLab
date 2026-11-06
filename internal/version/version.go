package version

// GitTag is reported by the GraphQL version field, dashboard and health API.
// Release tooling can override it with -ldflags -X for tagged builds.
var GitTag = "v0.10.1"
