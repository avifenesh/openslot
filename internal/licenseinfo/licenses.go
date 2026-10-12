// Package licenseinfo keeps dependency notices accessible in standalone installs.
package licenseinfo

import _ "embed"

//go:embed dependencies.txt
var Dependencies string

//go:embed vendored.txt
var Vendored string

//go:embed project.txt
var Project string
