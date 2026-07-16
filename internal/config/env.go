package config

import (
	"bufio"
	"log"
	"os"
	"strings"
)

// LoadEnv reads a .env file and sets any variables that are not already
// defined in the process environment. This respects container orchestrators
// (Docker, K8s) that inject env vars at runtime — those take precedence.
func LoadEnv(filenames ...string) {
	if len(filenames) == 0 {
		filenames = []string{".env"}
	}

	for _, filename := range filenames {
		if err := loadFile(filename); err != nil {
			// .env is optional — log and continue
			log.Printf("[config] No %s file found, using environment defaults", filename)
		}
	}
}

func loadFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Strip surrounding quotes (single or double)
		value = strings.Trim(value, `"'`)

		// Only set if not already defined (system/container env takes precedence)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

// GetEnv returns the value of an environment variable, or a fallback default.
func GetEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
