package cluster

import (
	dhtcclient "dhtc/dhtc-client"
	"testing"
)

func TestWorkerQueueIsBounded(t *testing.T) {
	sink, err := NewWorkerSink("http://master", "token", "worker-1", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	md := dhtcclient.Metadata{InfoHash: make([]byte, 20), Name: "test"}
	if !sink.Enqueue(md) || !sink.Enqueue(md) {
		t.Fatal("expected first two records to fit in queue")
	}
	if sink.Enqueue(md) {
		t.Fatal("expected full queue to reject metadata")
	}
	if sink.QueueLength() != 2 || sink.Dropped() != 1 {
		t.Fatalf("queue=%d dropped=%d, want 2 and 1", sink.QueueLength(), sink.Dropped())
	}
}

func TestWorkerConfigurationRequiresMasterAndToken(t *testing.T) {
	if _, err := NewWorkerSink("", "token", "worker", 1, 1); err == nil {
		t.Fatal("expected missing master URL error")
	}
	if _, err := NewWorkerSink("http://master", "", "worker", 1, 1); err == nil {
		t.Fatal("expected missing cluster token error")
	}
}
