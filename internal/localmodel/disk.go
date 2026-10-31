package localmodel

import (
	"os"
	"path/filepath"
)

// PullReserve is the disk space a pull must leave free, for the system's
// swap and updates and the user's own files.
const PullReserve = 10 * GiB

// ModelsDir is where an Ollama server keeps its models: OLLAMA_MODELS in
// env, the server's own settings, else in the environment, else
// ~/.ollama/models.
func ModelsDir(env map[string]string) string {
	if d := env["OLLAMA_MODELS"]; d != "" {
		return d
	}
	if d := os.Getenv("OLLAMA_MODELS"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ollama", "models")
}

// FreeDisk is the space a user's program may still write on the volume
// that holds path, or the nearest folder above it that exists.
func FreeDisk(path string) (int64, error) {
	for {
		free, err := freeDisk(path)
		if err == nil || !os.IsNotExist(err) {
			return free, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, err
		}
		path = parent
	}
}
