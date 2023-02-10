package util

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var flagsFromConfig = make(map[string]string)

func FlagDefault(name, dflt string) string {
	if s, ok := flagsFromConfig[name]; ok {
		return s
	}
	return dflt
}

func defaultFlagConfigFiles() []string {
	files := []string{"/etc/wig.conf"}
	if s := os.Getenv("HOME"); s != "" {
		files = append(files, filepath.Join(s, ".wig.conf"))
	}
	return files
}

func LoadFlagsFromConfig(overridePath string) error {
	// Errors loading the manually-specified config are fatal.
	if overridePath != "" {
		values, err := flagsFromFile(overridePath)
		if err != nil {
			return err
		}
		flagsFromConfig = values
		return nil
	}

	// Errors loading the default config files are ignored.
	for _, path := range defaultFlagConfigFiles() {
		if values, err := flagsFromFile(path); err == nil {
			flagsFromConfig = values
			break
		}
	}

	return nil
}

func flagsFromFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var lineNum int
	out := make(map[string]string)
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if n := strings.IndexByte(line, '#'); n >= 0 {
			line = line[:n]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			return nil, fmt.Errorf("syntax error (%s:%d): not in 'flag value' format", path, lineNum)
		}
		out[key] = value
	}
	return out, nil
}
