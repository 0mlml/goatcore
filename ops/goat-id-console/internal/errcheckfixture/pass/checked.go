//go:build errcheck_pass

package pass

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func writeFile(f *os.File) error {
	if _, err := f.Write([]byte("checked")); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func encodeJSON(w io.Writer, value any) error {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return nil
}

func writeHTTPResponse(w http.ResponseWriter) {
	_, _ = w.Write([]byte("best effort")) //nolint:errcheck // response delivery cannot be recovered after headers are committed
}
