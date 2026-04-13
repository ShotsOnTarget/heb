package main

// Version is set at build time via -ldflags.
// Falls back to "dev" for untagged builds.
var Version = "dev"
