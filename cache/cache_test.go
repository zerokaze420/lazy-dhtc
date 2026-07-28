package cache

import "testing"

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
