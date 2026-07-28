package cluster

import (
	"context"
	dhtcclient "dhtc/dhtc-client"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkerQueuePullAndAck(t *testing.T) {
	queue := NewWorkerQueue("edge-1", 2)
	md := dhtcclient.Metadata{InfoHash: make([]byte, 20), Name: "test", Family: 6}
	if !queue.Enqueue(md) || !queue.Enqueue(md) || queue.Enqueue(md) {
		t.Fatal("worker queue capacity was not enforced")
	}
	server := httptest.NewServer(queue.Handler("secret", 1))
	defer server.Close()

	pull := func() Batch {
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var batch Batch
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			t.Fatal(err)
		}
		return batch
	}
	first := pull()
	second := pull()
	if len(first.Items) != 1 || second.Items[0].ID != first.Items[0].ID {
		t.Fatal("unacknowledged record was not retained")
	}

	puller, err := NewMasterPuller([]string{server.URL}, "secret", func(md dhtcclient.Metadata) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := puller.pull(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	if queue.Length() != 1 {
		t.Fatalf("queue length = %d, want 1 after ack", queue.Length())
	}
}

func TestWorkerQueueRequiresToken(t *testing.T) {
	queue := NewWorkerQueue("edge-1", 1)
	req := httptest.NewRequest(http.MethodGet, QueuePath, nil)
	recorder := httptest.NewRecorder()
	queue.Handler("secret", 1).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMasterPullerRequiresToken(t *testing.T) {
	if _, err := NewMasterPuller([]string{"http://worker"}, "", func(dhtcclient.Metadata) bool { return true }); err == nil {
		t.Fatal("expected missing cluster token error")
	}
}
