package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// dataDirName is the folder under the user's application support directory
// that holds everything the app keeps (decision D5).
const dataDirName = "Sev1Situations"

// DataDir returns the application data directory, which holds reports,
// settings, user scenarios and the release cache (R-STATE-4). It is
// ~/Library/Application Support/Sev1Situations. Setting S1S_DATA_DIR overrides
// it, for tests and for trying the app without touching the real one.
func DataDir() (string, error) {
	if dir := os.Getenv("S1S_DATA_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the application support directory: %w", err)
	}
	return filepath.Join(base, dataDirName), nil
}

// UserScenarioDir returns the directory where the user's own scenarios live
// (R-SCN-3).
func UserScenarioDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "scenarios"), nil
}
