//go:build errcheck_reject

package reject

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

func fileNamedF(f *os.File) {
	_, _ = f.Write([]byte("unchecked"))
}

func fileNamedW(w *os.File) {
	_, _ = w.Write([]byte("unchecked"))
}

func encodeJSON(w io.Writer, value any) {
	_ = json.NewEncoder(w).Encode(value)
}

func copyBuffer(buf *bytes.Buffer, w io.Writer) {
	_, _ = buf.WriteTo(w)
}
