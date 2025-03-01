package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	batcherv1 "github.com/dynoinc/skyvault/gen/proto/batcher/v1"
	batcherv1connect "github.com/dynoinc/skyvault/gen/proto/batcher/v1/v1connect"
	indexv1 "github.com/dynoinc/skyvault/gen/proto/index/v1"
	indexv1connect "github.com/dynoinc/skyvault/gen/proto/index/v1/v1connect"
	"github.com/influxdata/tdigest"
	"github.com/olekukonko/tablewriter"
)

// Configuration options for the stress test
type config struct {
	Service     string
	Concurrency int
	Duration    time.Duration
	KeySize     int
	ValueSize   int
	BatchSize   int
	Namespace   string
	BatcherName string
	IndexName   string
	BatcherPort int
	IndexPort   int
	ReadMode    string // For index service: "written", "notwritten", "deleted", "mixed"
	Debug       bool   // Enable debug output
}

// Service represents a simplified Kubernetes service structure
type Service struct {
	Spec struct {
		Ports []struct {
			Port int `json:"port"`
		} `json:"ports"`
	} `json:"spec"`
}

// Metrics collected during the test
type metrics struct {
	sync.Mutex
	serviceName  string
	requestCount int64
	errorCount   int64
	startTime    time.Time
	endTime      time.Time
	minLatency   float64
	maxLatency   float64
	totalLatency float64
	digest       *tdigest.TDigest
}

func newMetrics(serviceName string) *metrics {
	return &metrics{
		serviceName: serviceName,
		minLatency:  float64(time.Hour), // Start with a large value
		digest:      tdigest.NewWithCompression(100),
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
	m.requestCount++
}

func (m *metrics) calculatePercentile(p float64) float64 {
	m.Lock()
	defer m.Unlock()
	return m.digest.Quantile(p / 100)
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Service, "service", "both", "Service to stress test (batcher, index, both)")
	flag.IntVar(&cfg.Concurrency, "concurrency", 10, "Number of concurrent clients")
	flag.DurationVar(&cfg.Duration, "duration", 10*time.Second, "Test duration")
	flag.IntVar(&cfg.KeySize, "key-size", 16, "Size of keys in bytes")
	flag.IntVar(&cfg.ValueSize, "value-size", 100, "Size of values in bytes")
	flag.IntVar(&cfg.BatchSize, "batch-size", 10, "Number of keys in each batch")
	flag.StringVar(&cfg.Namespace, "namespace", "default", "Kubernetes namespace")
	flag.StringVar(&cfg.BatcherName, "batcher-name", "skyvault-batcher", "Kubernetes batcher service name")
	flag.StringVar(&cfg.IndexName, "index-name", "skyvault-index", "Kubernetes index service name")
	flag.StringVar(&cfg.ReadMode, "read-mode", "mixed", "For index service: written, notwritten, deleted, mixed")
	flag.BoolVar(&cfg.Debug, "debug", true, "Enable debug output")
	flag.Parse()

	// Setup for tracking metrics for each service
	allMetrics := []*metrics{}

	// Test batcher if specified
	var batcherTarget string
	var indexTarget string
	var writtenKeys []string   // Track written keys for index service testing
	var deletedKeys []string   // Track deleted keys for index service testing
	var unwrittenKeys []string // Track unwritten keys for index service testing

	if cfg.Service == "batcher" || cfg.Service == "both" {
		// Discover the batcher service port from Kubernetes
		batcherPort, err := getK8sServicePort(cfg.Namespace, cfg.BatcherName)
		if err != nil {
			log.Fatalf("Failed to discover batcher service port: %v", err)
		}

		fmt.Printf("Discovered Kubernetes batcher service port: %d\n", batcherPort)
		cfg.BatcherPort = batcherPort

		// Setup port-forwarding to Kubernetes for batcher
		fmt.Printf("Setting up port-forwarding to %s.%s on local port %d\n",
			cfg.BatcherName, cfg.Namespace, cfg.BatcherPort)

		batcherTarget = fmt.Sprintf("http://localhost:%d", cfg.BatcherPort)

		// Start port-forwarding for batcher
		batcherPortForwardCmd := exec.Command("kubectl", "port-forward",
			fmt.Sprintf("service/%s", cfg.BatcherName),
			fmt.Sprintf("%d:%d", cfg.BatcherPort, batcherPort),
			"-n", cfg.Namespace)

		batcherPortForwardCmd.Stdout = io.Discard
		batcherPortForwardCmd.Stderr = os.Stderr

		err = batcherPortForwardCmd.Start()
		if err != nil {
			log.Fatalf("Failed to start port-forwarding for batcher: %v", err)
		}

		defer func() {
			if batcherPortForwardCmd.Process != nil {
				fmt.Println("Stopping batcher port-forwarding...")
				batcherPortForwardCmd.Process.Signal(os.Interrupt)
				batcherPortForwardCmd.Wait()
			}
		}()

		// Try simple HTTP connection to check if batcher service is ready
		fmt.Println("Waiting for batcher service to be ready...")
		waitForServiceReady(batcherTarget)
	}

	// Setup index service if needed
	if cfg.Service == "index" || cfg.Service == "both" {
		// Discover the index service port from Kubernetes
		indexPort, err := getK8sServicePort(cfg.Namespace, cfg.IndexName)
		if err != nil {
			log.Fatalf("Failed to discover index service port: %v", err)
		}

		fmt.Printf("Discovered Kubernetes index service port: %d\n", indexPort)
		cfg.IndexPort = indexPort

		// Setup port-forwarding to Kubernetes for index service
		fmt.Printf("Setting up port-forwarding to %s.%s on local port %d\n",
			cfg.IndexName, cfg.Namespace, cfg.IndexPort)

		indexTarget = fmt.Sprintf("http://localhost:%d", cfg.IndexPort)

		// Start port-forwarding for index
		indexPortForwardCmd := exec.Command("kubectl", "port-forward",
			fmt.Sprintf("service/%s", cfg.IndexName),
			fmt.Sprintf("%d:%d", cfg.IndexPort, indexPort),
			"-n", cfg.Namespace)

		indexPortForwardCmd.Stdout = io.Discard
		indexPortForwardCmd.Stderr = os.Stderr

		err = indexPortForwardCmd.Start()
		if err != nil {
			log.Fatalf("Failed to start port-forwarding for index: %v", err)
		}

		defer func() {
			if indexPortForwardCmd.Process != nil {
				fmt.Println("Stopping index port-forwarding...")
				indexPortForwardCmd.Process.Signal(os.Interrupt)
				indexPortForwardCmd.Wait()
			}
		}()

		// Try simple HTTP connection to check if index service is ready
		fmt.Println("Waiting for index service to be ready...")
		waitForServiceReady(indexTarget)
	}

	// Create base context
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

	// Print test configuration
	fmt.Printf("Stress testing service(s): %s\n", cfg.Service)
	fmt.Printf("Concurrency: %d, Duration: %s\n", cfg.Concurrency, cfg.Duration)
	fmt.Printf("Key size: %d bytes, Value size: %d bytes, Batch size: %d\n",
		cfg.KeySize, cfg.ValueSize, cfg.BatchSize)
	if cfg.Service == "index" || cfg.Service == "both" {
		fmt.Printf("Index service read mode: %s\n", cfg.ReadMode)
	}
	fmt.Println("Press Ctrl+C to stop the test early")
	fmt.Println()

	// Generate test data for tracking
	testDataSize := cfg.BatchSize * 3 // We'll use 3x batch size to have enough test data
	writtenKeys = make([]string, testDataSize)
	unwrittenKeys = make([]string, testDataSize)
	deletedKeys = make([]string, testDataSize)

	// Generate unique keys for testing using valid UTF-8 characters only
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for i := 0; i < testDataSize; i++ {
		// Create key with proper prefix and valid characters
		prefix := fmt.Sprintf("W%04d_", i) // W for "write" keys with unique index
		keyLength := max(cfg.KeySize, len(prefix))

		key := make([]byte, keyLength)
		copy(key, prefix)

		// Fill the rest with valid characters
		for j := len(prefix); j < keyLength; j++ {
			key[j] = validChars[j%len(validChars)]
		}
		writtenKeys[i] = string(key)

		// Make unwritten keys with different prefix
		prefixU := fmt.Sprintf("U%04d_", i) // U for "unwritten" keys
		unwrittenKey := make([]byte, keyLength)
		copy(unwrittenKey, prefixU)
		for j := len(prefixU); j < keyLength; j++ {
			unwrittenKey[j] = validChars[j%len(validChars)]
		}
		unwrittenKeys[i] = string(unwrittenKey)

		// Make deleted keys with different prefix
		prefixD := fmt.Sprintf("D%04d_", i) // D for "deleted" keys
		deletedKey := make([]byte, keyLength)
		copy(deletedKey, prefixD)
		for j := len(prefixD); j < keyLength; j++ {
			deletedKey[j] = validChars[j%len(validChars)]
		}
		deletedKeys[i] = string(deletedKey)
	}

	// For tracking which keys were actually written or deleted
	actualWrittenKeys := make(map[string]bool)
	actualDeletedKeys := make(map[string]bool)

	// Run the batcher test if requested
	if cfg.Service == "batcher" || cfg.Service == "both" {
		// Create metrics collector for batcher
		batcherMetrics := newMetrics("batcher")
		batcherMetrics.startTime = time.Now()

		// Run the batcher write test
		stressBatcher(ctx, cfg, batcherTarget, batcherMetrics, writtenKeys, deletedKeys, actualWrittenKeys, actualDeletedKeys)

		batcherMetrics.endTime = time.Now()
		allMetrics = append(allMetrics, batcherMetrics)
	}

	// Run the index test if requested
	if cfg.Service == "index" || cfg.Service == "both" {
		// Create metrics collector for index
		indexMetrics := newMetrics("index")
		indexMetrics.startTime = time.Now()

		// If we're testing both services, add a small delay to allow writes to propagate
		if cfg.Service == "both" {
			time.Sleep(1 * time.Second)
		}

		// Run the index read test
		stressIndex(ctx, cfg, indexTarget, indexMetrics, actualWrittenKeys, unwrittenKeys, actualDeletedKeys)

		indexMetrics.endTime = time.Now()
		allMetrics = append(allMetrics, indexMetrics)
	}

	// Print results
	printResults(allMetrics)
}

// waitForServiceReady tries to establish a connection to the service
func waitForServiceReady(target string) {
	for i := 0; i < 10; i++ {
		client := &http.Client{Timeout: time.Second}
		req, _ := http.NewRequestWithContext(context.Background(), "GET", target, nil)
		_, err := client.Do(req)
		if err == nil {
			fmt.Println("Service is available")
			break
		}
		if i == 9 {
			log.Printf("Warning: Service connection check failed after multiple attempts: %v", err)
			log.Println("Continuing anyway, but expect possible connection issues...")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// getK8sServicePort retrieves the port for a Kubernetes service
func getK8sServicePort(namespace, serviceName string) (int, error) {
	// Use kubectl to get the service information in JSON format
	cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("kubectl error: %s: %s", err, exitErr.Stderr)
		}
		return 0, fmt.Errorf("failed to execute kubectl: %v", err)
	}

	// Parse the JSON output
	var service Service
	if err := json.Unmarshal(output, &service); err != nil {
		return 0, fmt.Errorf("failed to parse kubectl output: %v", err)
	}

	// Extract the port
	if len(service.Spec.Ports) == 0 {
		return 0, fmt.Errorf("no ports found for service %s", serviceName)
	}

	// Return the first port
	return service.Spec.Ports[0].Port, nil
}

func stressBatcher(ctx context.Context, cfg config, target string, m *metrics, writtenKeys, deletedKeys []string, actualWrittenKeys, actualDeletedKeys map[string]bool) {
	client := batcherv1connect.NewBatcherServiceClient(
		http.DefaultClient,
		target,
	)

	// Generate test data - valid UTF-8 value
	value := make([]byte, cfg.ValueSize)
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for i := range value {
		value[i] = validChars[i%len(validChars)]
	}

	// Create a WaitGroup to manage workers
	var wg sync.WaitGroup
	wg.Add(cfg.Concurrency)

	// Setup a timer to stop the test after the specified duration
	timer := time.NewTimer(cfg.Duration)
	stop := make(chan struct{})

	// For tracking keys to test in the index service
	keysMutex := sync.Mutex{}

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

			// We'll alternate between writing values and deleting them based on worker ID
			// Even workers will write, odd workers will delete
			isWriter := id%2 == 0

			for {
				select {
				case <-stop:
					return
				default:
					start := time.Now()
					var err error

					if isWriter {
						// Prepare a batch of write requests
						writes := make([]*batcherv1.WriteRequest, cfg.BatchSize)
						// Track keys for this batch
						batchKeys := make([]string, cfg.BatchSize)

						for j := range writes {
							// Use keys from the written keys list
							keyIndex := (id*cfg.BatchSize + j) % len(writtenKeys)
							keyStr := writtenKeys[keyIndex]
							batchKeys[j] = keyStr

							writes[j] = batcherv1.WriteRequest_builder{
								Key: &keyStr,
								Put: value,
							}.Build()
						}

						req := batcherv1.BatchWriteRequest_builder{
							Writes: writes,
						}.Build()

						_, err = client.BatchWrite(ctx, connect.NewRequest(req))

						// If successful, record these keys as written
						if err == nil {
							keysMutex.Lock()
							for _, key := range batchKeys {
								actualWrittenKeys[key] = true
							}
							keysMutex.Unlock()
						}
					} else {
						// Prepare a batch of delete requests
						deletes := make([]*batcherv1.WriteRequest, cfg.BatchSize)
						// Track keys for this batch
						batchKeys := make([]string, cfg.BatchSize)

						for j := range deletes {
							// Use keys from the deleted keys list
							keyIndex := (id*cfg.BatchSize + j) % len(deletedKeys)
							keyStr := deletedKeys[keyIndex]
							batchKeys[j] = keyStr
							deleteTrue := true

							deletes[j] = batcherv1.WriteRequest_builder{
								Key:    &keyStr,
								Delete: &deleteTrue,
							}.Build()
						}

						req := batcherv1.BatchWriteRequest_builder{
							Writes: deletes,
						}.Build()

						_, err = client.BatchWrite(ctx, connect.NewRequest(req))

						// If successful, record these keys as deleted
						if err == nil {
							keysMutex.Lock()
							for _, key := range batchKeys {
								actualDeletedKeys[key] = true
							}
							keysMutex.Unlock()
						}
					}

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

func stressIndex(ctx context.Context, cfg config, target string, m *metrics, actualWrittenKeys map[string]bool, unwrittenKeys []string, actualDeletedKeys map[string]bool) {
	client := indexv1connect.NewIndexServiceClient(
		http.DefaultClient,
		target,
	)

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

	// Convert maps to slices for easier access
	writtenKeysList := make([]string, 0, len(actualWrittenKeys))
	for key := range actualWrittenKeys {
		writtenKeysList = append(writtenKeysList, key)
	}

	deletedKeysList := make([]string, 0, len(actualDeletedKeys))
	for key := range actualDeletedKeys {
		deletedKeysList = append(deletedKeysList, key)
	}

	fmt.Printf("Testing index with %d written keys, %d deleted keys, and %d unwritten keys\n",
		len(writtenKeysList), len(deletedKeysList), len(unwrittenKeys))

	// Function to verify the correctness of the result
	verifyResult := func(key string, result *indexv1.Result) bool {
		// Check if key is in written list
		isWritten := actualWrittenKeys[key]

		// Check if key is in deleted list
		isDeleted := actualDeletedKeys[key]

		// Verify the result is as expected
		if isWritten && !isDeleted {
			return result.HasFound()
		} else if isDeleted {
			return result.HasDeleted()
		} else {
			return result.HasNotFound()
		}
	}

	// For tracking issues with verification
	var verificationErrorCount int64
	var incorrectWritten int64
	var incorrectDeleted int64
	var incorrectNotFound int64
	var mutex sync.Mutex

	// Start workers
	for i := 0; i < cfg.Concurrency; i++ {
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-stop:
					return
				default:
					// Prepare a batch of keys to read based on the read mode
					keys := make([]string, cfg.BatchSize)

					for j := range keys {
						switch cfg.ReadMode {
						case "written":
							// Only read keys that were written
							if len(writtenKeysList) > 0 {
								keyIndex := (id*cfg.BatchSize + j) % len(writtenKeysList)
								keys[j] = writtenKeysList[keyIndex]
							} else {
								// Fallback to unwritten keys if no written keys available
								keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
								keys[j] = unwrittenKeys[keyIndex]
							}
						case "notwritten":
							// Only read keys that were not written
							keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
							keys[j] = unwrittenKeys[keyIndex]
						case "deleted":
							// Only read keys that were deleted
							if len(deletedKeysList) > 0 {
								keyIndex := (id*cfg.BatchSize + j) % len(deletedKeysList)
								keys[j] = deletedKeysList[keyIndex]
							} else {
								// Fallback to unwritten keys if no deleted keys available
								keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
								keys[j] = unwrittenKeys[keyIndex]
							}
						case "mixed":
							// Mix of all types based on the remainder when divided by 3
							switch (id*cfg.BatchSize + j) % 3 {
							case 0:
								// Written
								if len(writtenKeysList) > 0 {
									keyIndex := (id*cfg.BatchSize + j) % len(writtenKeysList)
									keys[j] = writtenKeysList[keyIndex]
								} else {
									keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
									keys[j] = unwrittenKeys[keyIndex]
								}
							case 1:
								// Unwritten
								keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
								keys[j] = unwrittenKeys[keyIndex]
							case 2:
								// Deleted
								if len(deletedKeysList) > 0 {
									keyIndex := (id*cfg.BatchSize + j) % len(deletedKeysList)
									keys[j] = deletedKeysList[keyIndex]
								} else {
									keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
									keys[j] = unwrittenKeys[keyIndex]
								}
							}
						default:
							// Default to mixed if invalid read mode
							switch (id*cfg.BatchSize + j) % 3 {
							case 0:
								if len(writtenKeysList) > 0 {
									keyIndex := (id*cfg.BatchSize + j) % len(writtenKeysList)
									keys[j] = writtenKeysList[keyIndex]
								} else {
									keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
									keys[j] = unwrittenKeys[keyIndex]
								}
							case 1:
								keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
								keys[j] = unwrittenKeys[keyIndex]
							case 2:
								if len(deletedKeysList) > 0 {
									keyIndex := (id*cfg.BatchSize + j) % len(deletedKeysList)
									keys[j] = deletedKeysList[keyIndex]
								} else {
									keyIndex := (id*cfg.BatchSize + j) % len(unwrittenKeys)
									keys[j] = unwrittenKeys[keyIndex]
								}
							}
						}
					}

					req := indexv1.GetRequest_builder{
						Keys: keys,
					}.Build()

					start := time.Now()
					resp, err := client.Get(ctx, connect.NewRequest(req))
					latency := time.Since(start).Seconds() * 1000 // convert to ms

					if err != nil {
						fmt.Printf("Error: %v\n", err)
						m.addError()
					} else {
						// Verify results are as expected
						results := resp.Msg.GetResults()
						allCorrect := true
						incorrectCount := 0

						if len(results) != len(keys) {
							// Number of results doesn't match number of keys
							allCorrect = false
							incorrectCount = len(keys) - len(results)
							if incorrectCount < 0 {
								incorrectCount = 0
							}
						} else {
							for i, result := range results {
								if !verifyResult(keys[i], result) {
									allCorrect = false
									incorrectCount++

									// Track details about the verification failure for debugging
									if cfg.Debug {
										keyStatus := "unknown"
										resultStatus := "unknown"

										// Determine expected status
										if actualWrittenKeys[keys[i]] && !actualDeletedKeys[keys[i]] {
											keyStatus = "written"
											mutex.Lock()
											incorrectWritten++
											mutex.Unlock()
										} else if actualDeletedKeys[keys[i]] {
											keyStatus = "deleted"
											mutex.Lock()
											incorrectDeleted++
											mutex.Unlock()
										} else {
											keyStatus = "notfound"
											mutex.Lock()
											incorrectNotFound++
											mutex.Unlock()
										}

										// Determine actual result
										if result.HasFound() {
											resultStatus = "found"
										} else if result.HasDeleted() {
											resultStatus = "deleted"
										} else if result.HasNotFound() {
											resultStatus = "notfound"
										}

										fmt.Printf("Verification error: key=%s, expected=%s, got=%s\n",
											keys[i], keyStatus, resultStatus)
									}
								}
							}
						}

						if allCorrect {
							m.addLatency(latency)
						} else {
							// Track verification issues for debugging
							mutex.Lock()
							verificationErrorCount += int64(incorrectCount)
							mutex.Unlock()
							m.addError()
						}
					}
				}
			}
		}(i)
	}

	// Wait for all workers to complete
	wg.Wait()

	// Print verification error information if there were any
	if verificationErrorCount > 0 {
		fmt.Printf("Index verification errors: %d total\n", verificationErrorCount)
		if cfg.Debug {
			fmt.Printf("  Written keys with incorrect result: %d\n", incorrectWritten)
			fmt.Printf("  Deleted keys with incorrect result: %d\n", incorrectDeleted)
			fmt.Printf("  NotFound keys with incorrect result: %d\n", incorrectNotFound)
		}
	}
}

func printResults(allMetrics []*metrics) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Service", "Total", "Success", "Failed", "Throughput", "Min", "Max", "Avg", "P50", "P90", "P99"})
	table.SetBorder(true)

	for _, m := range allMetrics {
		duration := m.endTime.Sub(m.startTime).Seconds()

		table.Append([]string{
			m.serviceName,
			fmt.Sprintf("%d", m.requestCount),
			fmt.Sprintf("%d (%.2f%%)", m.requestCount-m.errorCount, 100-float64(m.errorCount)/float64(max(m.requestCount, 1))*100),
			fmt.Sprintf("%d (%.2f%%)", m.errorCount, float64(m.errorCount)/float64(max(m.requestCount, 1))*100),
			fmt.Sprintf("%.2f/s", float64(m.requestCount)/duration),
			fmt.Sprintf("%.2fms", m.minLatency),
			fmt.Sprintf("%.2fms", m.maxLatency),
			fmt.Sprintf("%.2fms", m.totalLatency/float64(max(m.requestCount, 1))),
			fmt.Sprintf("%.2fms", m.calculatePercentile(50)),
			fmt.Sprintf("%.2fms", m.calculatePercentile(90)),
			fmt.Sprintf("%.2fms", m.calculatePercentile(99)),
		})
	}

	fmt.Println("\nTest Results:")
	table.Render()
}
