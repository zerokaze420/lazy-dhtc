package cluster

import (
	"compress/gzip"
	"context"
	dhtcclient "dhtc/dhtc-client"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkerQueuePullAndAck(t *testing.T) {
	queue := NewWorkerQueue("edge-1", 2, nil)
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
	if queue.Length() != 0 {
		t.Fatalf("queue length = %d, want 0 after draining batches", queue.Length())
	}
	status := puller.Status()
	if len(status) != 1 || !status[0].Online || status[0].Pulled != 2 || status[0].Queued != 0 {
		t.Fatalf("worker status = %#v", status)
	}
}

func TestWorkerQueueCompressesBatchWhenRequested(t *testing.T) {
	queue := NewWorkerQueue("edge-gzip", 1, nil)
	if !queue.Enqueue(dhtcclient.Metadata{InfoHash: make([]byte, 20), Name: "compressible metadata"}) {
		t.Fatal("metadata was not enqueued")
	}
	req := httptest.NewRequest(http.MethodGet, QueuePath, nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	queue.Handler("secret", 1).ServeHTTP(recorder, req)

	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("content encoding = %q, want gzip", recorder.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var batch Batch
	if err := json.NewDecoder(reader).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 1 || batch.WorkerID != "edge-gzip" {
		t.Fatalf("compressed batch = %#v", batch)
	}
}

func TestWorkerQueueGzipResponse(t *testing.T) {
	queue := NewWorkerQueue("edge-1", 1, nil)
	if !queue.Enqueue(dhtcclient.Metadata{InfoHash: make([]byte, 20), Name: "compressed metadata"}) {
		t.Fatal("metadata was not enqueued")
	}
	req := httptest.NewRequest(http.MethodGet, QueuePath, nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	queue.Handler("secret", 1).ServeHTTP(recorder, req)
	if recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("content encoding = %q, want gzip", recorder.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(recorder.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var batch Batch
	if err := json.NewDecoder(reader).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 1 || batch.Items[0].Metadata.Name != "compressed metadata" {
		t.Fatalf("unexpected batch: %#v", batch)
	}
}

func TestMasterPullerReportsEmptyWorkerOnline(t *testing.T) {
	queue := NewWorkerQueue("edge-empty", 2, nil)
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
	queue := NewWorkerQueue("edge-1", 1, nil)
	req := httptest.NewRequest(http.MethodGet, QueuePath, nil)
	recorder := httptest.NewRecorder()
	queue.Handler("secret", 1).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestWorkerQueueAckNotifiesReleasedMetadata(t *testing.T) {
	released := make(chan dhtcclient.Metadata, 1)
	queue := NewWorkerQueue("edge-1", 1, func(md dhtcclient.Metadata) { released <- md })
	md := dhtcclient.Metadata{InfoHash: []byte("12345678901234567890")}
	if !queue.Enqueue(md) {
		t.Fatal("metadata was not enqueued")
	}
	batch := queue.Batch(1)
	queue.Ack([]uint64{batch.Items[0].ID})

	select {
	case got := <-released:
		if string(got.InfoHash) != string(md.InfoHash) {
			t.Fatalf("released hash = %q, want %q", got.InfoHash, md.InfoHash)
		}
	default:
		t.Fatal("ack did not notify released metadata")
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

func TestMasterPullerAcceptsIPv6WorkerURL(t *testing.T) {
	puller, err := NewMasterPuller([]string{"http://[2001:db8::1]:4200"}, "secret", func(dhtcclient.Metadata) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	status := puller.Status()
	if len(status) != 1 || status[0].URL != "http://[2001:db8::1]:4200" {
		t.Fatalf("IPv6 worker URL status = %#v", status)
	}
}
