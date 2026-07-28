package cluster

import (
	"bytes"
	"context"
	"crypto/subtle"
	dhtcclient "dhtc/dhtc-client"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	Queued   int      `json:"queued"`
	Dropped  uint64   `json:"dropped"`
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
	return Batch{WorkerID: q.workerID, Items: items, Queued: len(q.items), Dropped: q.dropped.Load()}
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
	mu      sync.RWMutex
	status  map[string]WorkerStatus
}

type WorkerStatus struct {
	URL         string    `json:"url"`
	WorkerID    string    `json:"worker_id"`
	Online      bool      `json:"online"`
	Queued      int       `json:"queued"`
	Dropped     uint64    `json:"dropped"`
	Pulled      uint64    `json:"pulled"`
	LastSuccess time.Time `json:"last_success"`
	LastAttempt time.Time `json:"last_attempt"`
	LastError   string    `json:"last_error"`
}

func NewMasterPuller(workerURLs []string, token string, ingest func(dhtcclient.Metadata) bool) (*MasterPuller, error) {
	puller := &MasterPuller{client: &http.Client{Timeout: 15 * time.Second}, ingest: ingest, status: make(map[string]WorkerStatus)}
	puller.Configure(workerURLs, token)
	return puller, nil
}

func (p *MasterPuller) Configure(workerURLs []string, token string) {
	workers := make([]string, 0, len(workerURLs))
	for _, worker := range workerURLs {
		worker = strings.TrimRight(strings.TrimSpace(worker), "/")
		if worker == "" {
			continue
		}
		parsed, err := url.Parse(worker)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			log.Warn().Str("worker", worker).Msg("Ignoring invalid Worker URL")
			continue
		}
		workers = append(workers, worker)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	status := make(map[string]WorkerStatus, len(workers))
	for _, worker := range workers {
		current := p.status[worker]
		current.URL = worker
		status[worker] = current
	}
	p.workers = workers
	p.token = token
	p.status = status
}

func (p *MasterPuller) Status() []WorkerStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]WorkerStatus, 0, len(p.workers))
	now := time.Now()
	for _, worker := range p.workers {
		status := p.status[worker]
		status.Online = !status.LastSuccess.IsZero() && now.Sub(status.LastSuccess) < 30*time.Second
		result = append(result, status)
	}
	return result
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
	p.mu.RLock()
	workers := append([]string(nil), p.workers...)
	token := p.token
	p.mu.RUnlock()
	if token == "" {
		return
	}
	for _, worker := range workers {
		if err := p.pullWithToken(ctx, worker, token); err != nil {
			p.recordFailure(worker, err)
			log.Warn().Err(err).Str("worker", worker).Msg("Could not pull worker metadata")
		}
	}
}

func (p *MasterPuller) recordFailure(worker string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.status[worker]
	status.LastAttempt = time.Now()
	status.LastError = err.Error()
	p.status[worker] = status
}

func (p *MasterPuller) pull(ctx context.Context, worker string) error {
	p.mu.RLock()
	token := p.token
	p.mu.RUnlock()
	if token == "" {
		return fmt.Errorf("cluster token is required")
	}
	return p.pullWithToken(ctx, worker, token)
}

func (p *MasterPuller) pullWithToken(ctx context.Context, worker, token string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, worker+QueuePath, nil)
	req.Header.Set("Authorization", "Bearer "+token)
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
		p.recordSuccess(worker, batch, 0)
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
	ackReq.Header.Set("Authorization", "Bearer "+token)
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
	p.recordSuccess(worker, batch, len(ids))
	return nil
}

func (p *MasterPuller) recordSuccess(worker string, batch Batch, pulled int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.status[worker]
	status.WorkerID = batch.WorkerID
	status.Queued = batch.Queued - pulled
	if status.Queued < 0 {
		status.Queued = 0
	}
	status.Dropped = batch.Dropped
	status.Pulled += uint64(pulled)
	status.LastAttempt = time.Now()
	status.LastSuccess = status.LastAttempt
	status.LastError = ""
	status.Online = true
	p.status[worker] = status
}
