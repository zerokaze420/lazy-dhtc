package cache

import (
	"testing"
	"time"
)

func TestInfoHashCacheRemoveAllowsRetry(t *testing.T) {
	infoHash := "retryable-info-hash"
	InfoHashCacheRemove(infoHash)

	if !InfoHashCacheAdd(infoHash) {
		t.Fatal("first add was rejected")
	}
	if InfoHashCacheAdd(infoHash) {
		t.Fatal("duplicate add was accepted")
	}

	InfoHashCacheRemove(infoHash)
	if !InfoHashCacheAdd(infoHash) {
		t.Fatal("add after removal was rejected")
	}
	InfoHashCacheRemove(infoHash)
}

func TestInfoHashCacheExpiresForRetry(t *testing.T) {
	infoHash := "expiring-info-hash"
	InfoHashCacheRemove(infoHash)
	if !InfoHashCacheAdd(infoHash) {
		t.Fatal("first add was rejected")
	}
	InfoHashCacheExpireAfter(infoHash, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if !InfoHashCacheAdd(infoHash) {
		t.Fatal("expired hash was not accepted again")
	}
	InfoHashCacheRemove(infoHash)
}
