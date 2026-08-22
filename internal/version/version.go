// Package version carries build metadata stamped in at link time.
package version

// Version is overwritten with -ldflags by .github/workflows/release.yml. The
// default is what a plain `go build` produces.
//
// Commit and Date lived here too, left from GoReleaser. Nothing read either, so
// the linker dropped them and -X wrote into a variable that was not in the
// binary. A field nobody reads is not build metadata, it is a promise.
var Version = "dev"
