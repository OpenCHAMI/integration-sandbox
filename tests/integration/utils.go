package integration

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Port of scripts/load-images.sh for use in Go tests
// Returns loaded environment variables for passing to subprocesses
func LoadImages() (map[string]string, error) {
	images, ok := os.LookupEnv("IMAGES")
	if !ok {
		images = "default"
	}

	var manifest string
	if strings.Contains(images, "/") || strings.HasSuffix(images, ".env") {
		manifest = images
	} else {
		manifest = "../../images/" + images + ".env"
	}

	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil, fmt.Errorf("unable to open manifest '%s': %v", manifest, err)
	}
	ret := make(map[string]string)
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		if key != "" { // Failing silently is expected behavior?
			if override := os.Getenv(key); override != "" {
				value = override
			}
			ret[key] = value
		}
	}
	return ret, s.Err()
}
