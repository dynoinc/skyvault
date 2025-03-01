package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/dynoinc/skyvault/gen/proto/batcher/v1"
	v1connect "github.com/dynoinc/skyvault/gen/proto/batcher/v1/v1connect"
	"github.com/influxdata/tdigest"
	"github.com/olekukonko/tablewriter"
)

// Configuration options for the stress test
type config struct {
	Service     string
	Target      string
	Concurrency int
	Duration    time.Duration
	KeySize     int
	ValueSize   int
	BatchSize   int
}

// Metrics collected during the test
type metrics struct {
	sync.Mutex
	requestCount int64
	errorCount   int64
	startTime    time.Time
	endTime      time.Time
	minLatency   float64
	maxLatency   float64
	totalLatency float64
	digest       *tdigest.TDigest
}

func newMetrics() *metrics {
	return &metrics{
		minLatency: float64(time.Hour), // Start with a large value
		digest:     tdigest.NewWithCompression(100),
	}
}

func (m *metrics) addLatency(latency float64) {
	m.Lock()
	defer m.Unlock()

	m.requestCount++
	m.totalLatency += latency

	if latency < m.minLatency {
		m.minLatency = latency
	}
	if latency > m.maxLatency {
		m.maxLatency = latency
	}

	m.digest.Add(latency, 1)
}

func (m *metrics) addError() {
	m.Lock()
	defer m.Unlock()
	m.errorCount++
}

func (m *metrics) calculatePercentile(p float64) float64 {
	m.Lock()
	defer m.Unlock()
	return m.digest.Quantile(p / 100)
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Service, "service", "batcher", "Service to stress test (batcher)")
	flag.StringVar(&cfg.Target, "target", "http://localhost:5001", "Target host:port")
	flag.IntVar(&cfg.Concurrency, "concurrency", 10, "Number of concurrent clients")
	flag.DurationVar(&cfg.Duration, "duration", 10*time.Second, "Test duration")
	flag.IntVar(&cfg.KeySize, "key-size", 16, "Size of keys in bytes")
	flag.IntVar(&cfg.ValueSize, "value-size", 100, "Size of values in bytes")
	flag.IntVar(&cfg.BatchSize, "batch-size", 10, "Number of keys in each batch")
	flag.Parse()

	// Print test configuration
	fmt.Printf("Stress testing %s at %s\n", cfg.Service, cfg.Target)
	fmt.Printf("Concurrency: %d, Duration: %s\n", cfg.Concurrency, cfg.Duration)
	fmt.Printf("Key size: %d bytes, Value size: %d bytes, Batch size: %d\n",
		cfg.KeySize, cfg.ValueSize, cfg.BatchSize)
	fmt.Println("Press Ctrl+C to stop the test early")
	fmt.Println()

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT (Ctrl+C)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nInterrupted, stopping test...")
		cancel()
	}()

	// Create metrics collector
	m := newMetrics()
	m.startTime = time.Now()

	// Run the test
	switch cfg.Service {
	case "batcher":
		stressBatcher(ctx, cfg, m)
	default:
		log.Fatalf("Unknown service: %s", cfg.Service)
	}

	m.endTime = time.Now()

	// Print results
	printResults(m)
}

func stressBatcher(ctx context.Context, cfg config, m *metrics) {
	client := v1connect.NewBatcherServiceClient(
		http.DefaultClient,
		cfg.Target,
	)

	// Generate test data
	key := make([]byte, cfg.KeySize)
	value := make([]byte, cfg.ValueSize)
	for i := range key {
		key[i] = byte(i % 256)
		if i < len(value) {
			value[i] = byte((i + 1) % 256)
		}
	}

	writes := make([]*v1.WriteRequest, cfg.BatchSize)
	for i := range writes {
		customKey := make([]byte, len(key))
		copy(customKey, key)
		// Make each key unique
		customKey[0] = byte(i)
		// Convert byte slice to string pointer for the Key field
		keyStr := string(customKey)

		writes[i] = v1.WriteRequest_builder{
			Key: &keyStr,
			Put: value,
		}.Build()
	}

	// Create a WaitGroup to manage workers
	var wg sync.WaitGroup
	wg.Add(cfg.Concurrency)

	// Setup a timer to stop the test after the specified duration
	timer := time.NewTimer(cfg.Duration)
	stop := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			close(stop)
		case <-timer.C:
			close(stop)
		}
	}()

	// Start workers
	for i := 0; i < cfg.Concurrency; i++ {
		go func(id int) {
			defer wg.Done()

			req := v1.BatchWriteRequest_builder{
				Writes: writes,
			}.Build()

			for {
				select {
				case <-stop:
					return
				default:
					start := time.Now()

					_, err := client.BatchWrite(ctx, connect.NewRequest(req))
					latency := time.Since(start).Seconds() * 1000 // convert to ms

					if err != nil {
						m.addError()
					} else {
						m.addLatency(latency)
					}
				}
			}
		}(i)
	}

	// Wait for all workers to complete
	wg.Wait()
}

func printResults(m *metrics) {
	duration := m.endTime.Sub(m.startTime).Seconds()

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Total", "Success", "Failed", "Throughput", "Min", "Max", "Avg", "P50", "P90", "P99"})
	table.SetBorder(true)

	table.Append([]string{
		fmt.Sprintf("%d", m.requestCount),
		fmt.Sprintf("%d (%.2f%%)", m.requestCount-m.errorCount, 100-float64(m.errorCount)/float64(m.requestCount)*100),
		fmt.Sprintf("%d (%.2f%%)", m.errorCount, float64(m.errorCount)/float64(m.requestCount)*100),
		fmt.Sprintf("%.2f/s", float64(m.requestCount)/duration),
		fmt.Sprintf("%.2fms", m.minLatency),
		fmt.Sprintf("%.2fms", m.maxLatency),
		fmt.Sprintf("%.2fms", m.totalLatency/float64(m.requestCount)),
		fmt.Sprintf("%.2fms", m.calculatePercentile(50)),
		fmt.Sprintf("%.2fms", m.calculatePercentile(90)),
		fmt.Sprintf("%.2fms", m.calculatePercentile(99)),
	})

	fmt.Println("\nTest Results:")
	table.Render()
}
