package main

import "os"

// writeFile is a tiny test helper that intentionally lives in its own
// file to keep main_test.go focused on behavior assertions.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
