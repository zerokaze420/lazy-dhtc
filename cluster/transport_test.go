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
	status := puller.Status()
	if len(status) != 1 || !status[0].Online || status[0].Pulled != 1 || status[0].Queued != 1 {
		t.Fatalf("worker status = %#v", status)
	}
}

func TestMasterPullerReportsEmptyWorkerOnline(t *testing.T) {
	queue := NewWorkerQueue("edge-empty", 2)
	server := httptest.NewServer(queue.Handler("secret", 1))
	defer server.Close()
	puller, err := NewMasterPuller([]string{server.URL}, "secret", func(dhtcclient.Metadata) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if err := puller.pull(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	status := puller.Status()[0]
	if !status.Online || status.WorkerID != "edge-empty" || status.Queued != 0 {
		t.Fatalf("empty worker status = %#v", status)
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

func TestMasterPullerCanBeConfiguredDynamically(t *testing.T) {
	puller, err := NewMasterPuller(nil, "", func(dhtcclient.Metadata) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	puller.Configure([]string{"http://worker-1", "http://worker-2"}, "secret")
	if len(puller.Status()) != 2 {
		t.Fatalf("worker status count = %d, want 2", len(puller.Status()))
	}
	puller.Configure([]string{"http://worker-2"}, "new-secret")
	status := puller.Status()
	if len(status) != 1 || status[0].URL != "http://worker-2" {
		t.Fatalf("worker status after reconfigure = %#v", status)
	}
}
