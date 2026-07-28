package cache

import (
	"dhtc/db"
	"encoding/hex"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/rs/zerolog/log"
)

var (
	infoHashCache   = mapset.NewSet[string]()
	infoHashCacheMu sync.RWMutex
	infoHashExpiry  = make(map[string]time.Time)
)

const maxExpiringInfoHashes = 100000

func InfoHashCacheContains(infoHash string) bool {
	infoHashCacheMu.RLock()
	defer infoHashCacheMu.RUnlock()
	return infoHashCache.Contains(infoHash)
}

func InfoHashCacheAdd(infoHash string) bool {
	infoHashCacheMu.Lock()
	defer infoHashCacheMu.Unlock()
	if expiresAt, ok := infoHashExpiry[infoHash]; ok && !expiresAt.After(time.Now()) {
		delete(infoHashExpiry, infoHash)
		infoHashCache.Remove(infoHash)
	}
	return infoHashCache.Add(infoHash)
}

func InfoHashCacheRemove(infoHash string) {
	infoHashCacheMu.Lock()
	defer infoHashCacheMu.Unlock()
	delete(infoHashExpiry, infoHash)
	infoHashCache.Remove(infoHash)
}

func InfoHashCacheExpireAfter(infoHash string, ttl time.Duration) {
	infoHashCacheMu.Lock()
	defer infoHashCacheMu.Unlock()
	if !infoHashCache.Contains(infoHash) {
		return
	}
	now := time.Now()
	if len(infoHashExpiry) >= maxExpiringInfoHashes {
		for hash, expiresAt := range infoHashExpiry {
			if !expiresAt.After(now) {
				delete(infoHashExpiry, hash)
				infoHashCache.Remove(hash)
			}
		}
	}
	if len(infoHashExpiry) >= maxExpiringInfoHashes {
		for hash := range infoHashExpiry {
			delete(infoHashExpiry, hash)
			infoHashCache.Remove(hash)
			break
		}
	}
	infoHashExpiry[infoHash] = now.Add(ttl)
}

func PopulateInfoHashCacheFromDatabase(database db.Repository) {
	all, err := database.GetAllInfoHashes()
	if err != nil {
		log.Error().Err(err).Msg("could not get all info hashes from database")
		return
	}
	for _, ih := range all {
		h, err := hex.DecodeString(ih)
		if err != nil {
			continue
		}
		InfoHashCacheAdd(string(h))
	}

	infoHashCacheMu.RLock()
	cacheSize := infoHashCache.Cardinality()
	infoHashCacheMu.RUnlock()
	log.Debug().Msgf("info hash cache size %d elements", cacheSize)
}
