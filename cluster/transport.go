package cluster

import (
	"bytes"
	"context"
	"crypto/subtle"
	dhtcclient "dhtc/dhtc-client"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

const QueuePath = "/api/worker/v1/queue"

type Record struct {
	ID       uint64              `json:"id"`
	Metadata dhtcclient.Metadata `json:"metadata"`
}

type Batch struct {
	WorkerID string   `json:"worker_id"`
	Items    []Record `json:"items"`
}

type Ack struct {
	IDs []uint64 `json:"ids"`
}

type WorkerQueue struct {
	workerID string
	limit    int
	mu       sync.Mutex
	nextID   uint64
	items    []Record
	dropped  atomic.Uint64
}

func NewWorkerQueue(workerID string, limit int) *WorkerQueue {
	if workerID == "" {
		workerID = "worker"
	}
	if limit < 1 {
		limit = 256
	}
	return &WorkerQueue{workerID: workerID, limit: limit}
}

func (q *WorkerQueue) Enqueue(md dhtcclient.Metadata) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) >= q.limit {
		q.dropped.Add(1)
		log.Warn().Uint64("dropped", q.dropped.Load()).Msg("Worker metadata queue is full")
		return false
	}
	q.nextID++
	q.items = append(q.items, Record{ID: q.nextID, Metadata: md})
	return true
}

func (q *WorkerQueue) Batch(limit int) Batch {
	q.mu.Lock()
	defer q.mu.Unlock()
	if limit < 1 || limit > len(q.items) {
		limit = len(q.items)
	}
	items := append([]Record(nil), q.items[:limit]...)
	return Batch{WorkerID: q.workerID, Items: items}
}

func (q *WorkerQueue) Ack(ids []uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	acked := make(map[uint64]struct{}, len(ids))
	for _, id := range ids {
		acked[id] = struct{}{}
	}
	kept := q.items[:0]
	for _, item := range q.items {
		if _, ok := acked[item.ID]; !ok {
			kept = append(kept, item)
		}
	}
	q.items = kept
}

func (q *WorkerQueue) Length() int     { q.mu.Lock(); defer q.mu.Unlock(); return len(q.items) }
func (q *WorkerQueue) Dropped() uint64 { return q.dropped.Load() }

func (q *WorkerQueue) Handler(token string, maxBatch int) http.Handler {
	if maxBatch < 1 || maxBatch > 64 {
		maxBatch = 16
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(q.Batch(maxBatch))
		case http.MethodPost:
			var ack Ack
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&ack) != nil {
				http.Error(w, "invalid ack", http.StatusBadRequest)
				return
			}
			q.Ack(ack.IDs)
			_ = json.NewEncoder(w).Encode(map[string]int{"queued": q.Length()})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

type MasterPuller struct {
	workers []string
	token   string
	client  *http.Client
	ingest  func(dhtcclient.Metadata) bool
}

func NewMasterPuller(workerURLs []string, token string, ingest func(dhtcclient.Metadata) bool) (*MasterPuller, error) {
	if token == "" {
		return nil, fmt.Errorf("cluster token is required")
	}
	workers := make([]string, 0, len(workerURLs))
	for _, worker := range workerURLs {
		worker = strings.TrimRight(strings.TrimSpace(worker), "/")
		if worker != "" {
			workers = append(workers, worker)
		}
	}
	return &MasterPuller{workers: workers, token: token, client: &http.Client{Timeout: 15 * time.Second}, ingest: ingest}, nil
}

func (p *MasterPuller) Run(ctx context.Context) {
	p.pullAll(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pullAll(ctx)
		}
	}
}

func (p *MasterPuller) pullAll(ctx context.Context) {
	for _, worker := range p.workers {
		if err := p.pull(ctx, worker); err != nil {
			log.Warn().Err(err).Str("worker", worker).Msg("Could not pull worker metadata")
		}
	}
}

func (p *MasterPuller) pull(ctx context.Context, worker string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, worker+QueuePath, nil)
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("worker returned %d", resp.StatusCode)
	}
	var batch Batch
	if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
		return err
	}
	if len(batch.Items) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(batch.Items))
	for _, item := range batch.Items {
		if (len(item.Metadata.InfoHash) == 20 || len(item.Metadata.InfoHash) == 32) && p.ingest(item.Metadata) {
			ids = append(ids, item.ID)
		} else {
			ids = append(ids, item.ID)
		}
	}
	body, _ := json.Marshal(Ack{IDs: ids})
	ackReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, worker+QueuePath, bytes.NewReader(body))
	ackReq.Header.Set("Authorization", "Bearer "+p.token)
	ackReq.Header.Set("Content-Type", "application/json")
	ackResp, err := p.client.Do(ackReq)
	if err != nil {
		return err
	}
	defer ackResp.Body.Close()
	if ackResp.StatusCode/100 != 2 {
		return fmt.Errorf("worker ack returned %d", ackResp.StatusCode)
	}
	log.Info().Str("worker_id", batch.WorkerID).Int("items", len(ids)).Msg("Master pulled worker metadata")
	return nil
}
