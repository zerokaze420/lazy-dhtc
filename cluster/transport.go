package cluster

import (
	"bytes"
	"context"
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

const IngestPath = "/api/worker/v1/metadata"

type Batch struct {
	WorkerID string                `json:"worker_id"`
	Items    []dhtcclient.Metadata `json:"items"`
}

type BatchResponse struct {
	Accepted  int `json:"accepted"`
	Duplicate int `json:"duplicate"`
}

type WorkerSink struct {
	masterURL string
	token     string
	workerID  string
	batchSize int
	queue     chan dhtcclient.Metadata
	client    *http.Client
	dropped   atomic.Uint64
	mu        sync.Mutex
}

func NewWorkerSink(masterURL, token, workerID string, queueSize, batchSize int) (*WorkerSink, error) {
	if strings.TrimSpace(masterURL) == "" {
		return nil, fmt.Errorf("master URL is required in worker mode")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("cluster token is required in worker mode")
	}
	if queueSize < 1 {
		queueSize = 256
	}
	if batchSize < 1 || batchSize > queueSize {
		batchSize = min(16, queueSize)
	}
	if workerID == "" {
		workerID = "worker"
	}
	return &WorkerSink{
		masterURL: strings.TrimRight(masterURL, "/"),
		token:     token,
		workerID:  workerID,
		batchSize: batchSize,
		queue:     make(chan dhtcclient.Metadata, queueSize),
		client:    &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (s *WorkerSink) Enqueue(md dhtcclient.Metadata) bool {
	select {
	case s.queue <- md:
		return true
	default:
		s.dropped.Add(1)
		log.Warn().Uint64("dropped", s.dropped.Load()).Msg("Worker metadata queue is full")
		return false
	}
}

func (s *WorkerSink) QueueLength() int { return len(s.queue) }
func (s *WorkerSink) Dropped() uint64  { return s.dropped.Load() }

func (s *WorkerSink) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	pending := make([]dhtcclient.Metadata, 0, s.batchSize)
	for {
		input := s.queue
		if len(pending) >= s.batchSize {
			input = nil
		}
		select {
		case <-ctx.Done():
			return
		case md := <-input:
			pending = append(pending, md)
			if len(pending) >= s.batchSize && s.upload(ctx, pending) {
				pending = pending[:0]
			}
		case <-ticker.C:
			if len(pending) > 0 && s.upload(ctx, pending) {
				pending = pending[:0]
			}
		}
	}
}

func (s *WorkerSink) upload(ctx context.Context, items []dhtcclient.Metadata) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, err := json.Marshal(Batch{WorkerID: s.workerID, Items: items})
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.masterURL+IngestPath, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		log.Warn().Err(err).Msg("Worker could not upload metadata batch")
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		log.Warn().Int("status", resp.StatusCode).Msg("Master rejected metadata batch")
		return false
	}
	log.Info().Int("items", len(items)).Int("queued", len(s.queue)).Msg("Worker uploaded metadata batch")
	return true
}
