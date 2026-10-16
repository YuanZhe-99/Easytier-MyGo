package helper

import (
	"os"
	"path/filepath"
)

// BinDir holds the helper's copies of the bundle's executables.
var BinDir = filepath.Join(os.Getenv("ProgramFiles"), "EasyTier MyGo", "helper")

// StateDir is the helper's state (networks, events, settings, cores).
var StateDir = filepath.Join(os.Getenv("ProgramData"), "EasyTier MyGo", "state")
