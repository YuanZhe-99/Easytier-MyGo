// Package version holds the single product version shared by every binary.
package version

// Version is the product version. mygo.json must carry the same value
// (checked by TestManifestMatches); release tags are "v" + Version.
const Version = "0.1.0"
