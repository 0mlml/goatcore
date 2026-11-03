package server

import (
	"crypto/sha256"
	"encoding/hex"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// correlationID derives a short, stable identifier for an EncryptedMessage
// that can be grepped across signal-server node logs. It's stable across
// federation hops because the encrypted body is bitwise identical on every
// node that sees this message: A's inbound Send, A's outbound Forward, B's
// Forward handler, and B's delivery to the target peer's stream.
//
// Format: sha256(body)[:4] → 8 hex chars. Collisions are possible but at
// our signaling write rate (single-digit transactions per minute) we'll
// never see one. If that changes, widen the slice.
func correlationID(msg *signalpb.EncryptedMessage) string {
	body := msg.GetBody()
	if len(body) == 0 {
		return "no-body"
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:4])
}
