package server

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// TestRegistryConcurrent hammers register / unregister / deliver / count
// from many goroutines across a small key space. The goal is not to assert
// semantic correctness of individual operations (other tests do that); it's
// to run under `go test -race` and catch data-race regressions on the map
// and peerStream channel.
//
// Key space is deliberately small (10 keys, 100 goroutines) so re-register
// collisions are frequent — that's where the eviction-on-re-register race
// lives.
func TestRegistryConcurrent(t *testing.T) {
	r := newRegistry()

	const (
		goroutines = 100
		opsPerG    = 50
		keyCount   = 10
	)
	keys := make([]string, keyCount)
	for i := range keys {
		keys[i] = fmt.Sprintf("peer-%d", i)
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			var myRegistrations []struct {
				key string
				ps  *peerStream
			}
			for i := 0; i < opsPerG; i++ {
				key := keys[rng.Intn(keyCount)]
				switch rng.Intn(4) {
				case 0:
					ps := r.register(key)
					myRegistrations = append(myRegistrations, struct {
						key string
						ps  *peerStream
					}{key, ps})
				case 1:
					if len(myRegistrations) > 0 {
						idx := rng.Intn(len(myRegistrations))
						reg := myRegistrations[idx]
						r.unregister(reg.key, reg.ps)
						myRegistrations = append(myRegistrations[:idx], myRegistrations[idx+1:]...)
					}
				case 2:
					_ = r.deliver(key, &signalpb.EncryptedMessage{RemoteKey: key})
				case 3:
					_ = r.count()
				}
			}
		}(int64(g))
	}
	wg.Wait()
}
