package version

// Version is overridden at build time via -ldflags "-X .../internal/version.Version=v0.1.0".
var Version = "dev"
