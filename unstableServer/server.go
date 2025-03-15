package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"runtime"
	"sync"
	"time"
)

type ServerCrashConfig struct {
	HighCPULoadChance        float32 // Initial chance
	MemoryLeakChance         float32
	NetworkDelayChance       float32
	ResourceExhaustionChance float32
	BuildupDuration          time.Duration // Time to build up to full chaos (30-40 min)
	CrashWindow              time.Duration // Additional time to crash (total 30-45 min)
	InitialDelay             time.Duration // Initial delay before chaos starts (0-5 min)
}

type CrashState struct {
	BuildupStartTime time.Time
	ChaosEnabled     bool
	ChaosCount       int // Track intensity for each type
}

var (
	config = ServerCrashConfig{
		HighCPULoadChance:        0.05,
		MemoryLeakChance:         0.03,
		NetworkDelayChance:       0.06,
		ResourceExhaustionChance: 0.04,
		BuildupDuration:          35 * time.Minute,
		CrashWindow:              10 * time.Minute,
		InitialDelay:             time.Duration(rand.Intn(5*60)) * time.Second, // 0-5 min
	}
	memoryHog   []byte
	mu          sync.Mutex
	chaosStates = map[string]*CrashState{
		"HighCPULoad":        {BuildupStartTime: time.Time{}, ChaosEnabled: false, ChaosCount: 0},
		"MemoryLeak":         {BuildupStartTime: time.Time{}, ChaosEnabled: false, ChaosCount: 0},
		"NetworkDelay":       {BuildupStartTime: time.Time{}, ChaosEnabled: false, ChaosCount: 0},
		"ResourceExhaustion": {BuildupStartTime: time.Time{}, ChaosEnabled: false, ChaosCount: 0},
	}
	currentCrashType string
	lastChaosCheck   time.Time
	chaosInterval    = 5 * time.Second
	chaosStarted     bool
)

func main() {
	rand.Seed(time.Now().UnixNano())

	// Initialize server
	fmt.Printf("Starting unstable server on :8080... (Initial delay: %v)\n", config.InitialDelay)
	startServer()
}

func startServer() {
	// Create an HTTP server instance
	server := &http.Server{
		Addr:    ":8080",
		Handler: nil, // Will use default ServeMux
	}

	// Register handlers
	http.HandleFunc("/", chaosHandler)
	http.HandleFunc("/health", healthCheck)

	// Run server in a loop with recovery
	for {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server failed to start: %v\n", err)
			return
		}

		// Simulate a crash recovery (application-level restart)
		fmt.Println("Server crashed, restarting at application level...")
		if currentCrashType != "" {
			// Reset the specific crash type state
			chaosStates[currentCrashType] = &CrashState{
				BuildupStartTime: time.Time{},
				ChaosEnabled:     false,
				ChaosCount:       0,
			}
			currentCrashType = "" // Allow a new crash type to be selected
		}
		chaosStarted = false // Reset chaos start flag
		time.Sleep(2 * time.Second)

		// Simulate server restart
		fmt.Printf("Restarting server... (Initial delay: %v)\n", config.InitialDelay)
	}
}

func chaosHandler(w http.ResponseWriter, r *http.Request) {
	// Check for chaos every chaosInterval
	if time.Since(lastChaosCheck) > chaosInterval {
		lastChaosCheck = time.Now()

		// Wait for initial delay before starting chaos
		if !chaosStarted && time.Since(time.Now().Add(-config.InitialDelay)) < 0 {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Server is initializing... (Remaining delay: %v)", time.Until(time.Now().Add(config.InitialDelay)))
			return
		}

		// Start chaos after initial delay and set buildup start time
		if !chaosStarted {
			chaosStarted = true
			for _, state := range chaosStates {
				state.BuildupStartTime = time.Now()
			}
			fmt.Println("Chaos initialization started after initial delay")
			return // Skip chaos on the first check to avoid immediate simulation
		}

		// Chaos Monkey: Select one crash type if none is active, but only after stabilization
		if currentCrashType == "" && time.Since(chaosStates["HighCPULoad"].BuildupStartTime) > 10*time.Second {
			selectCrashType()
		}

		// If a crash type is active, simulate its buildup
		if currentCrashType != "" {
			state := chaosStates[currentCrashType]
			elapsed := time.Since(state.BuildupStartTime)

			// Gradually increase failure intensity over BuildupDuration
			progress := float32(elapsed.Seconds()) / float32(config.BuildupDuration.Seconds())
			if progress > 1.0 {
				progress = 1.0
			}

			// Adjust probability based on progress
			var effectiveChance float32
			switch currentCrashType {
			case "HighCPULoad":
				effectiveChance = config.HighCPULoadChance * progress * 6 // Up to 0.3
			case "MemoryLeak":
				effectiveChance = config.MemoryLeakChance * progress * 6 // Up to 0.2
			case "NetworkDelay":
				effectiveChance = config.NetworkDelayChance * progress * 6 // Up to 0.4
			case "ResourceExhaustion":
				effectiveChance = config.ResourceExhaustionChance * progress * 6 // Up to 0.25
			}

			// Trigger chaos if probability is met
			if !state.ChaosEnabled || rand.Float32() < effectiveChance {
				state.ChaosEnabled = true
				state.ChaosCount++

				// Simulate crash by closing the connection after 30-45 minutes
				if elapsed > (config.BuildupDuration + config.CrashWindow) {
					fmt.Printf("Server crashed due to sustained %s!\n", currentCrashType)
					if hijacker, ok := w.(http.Hijacker); ok {
						conn, _, err := hijacker.Hijack()
						if err == nil {
							conn.Close()
						}
					}
					return
				}

				// Simulate the chaos type
				switch currentCrashType {
				case "HighCPULoad":
					simulateHighCPU(elapsed)
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, "Error: Server failed due to High CPU Load buildup (Count: %d)", state.ChaosCount)
					return
				case "MemoryLeak":
					simulateMemoryLeak(elapsed)
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, "Error: Server failed due to Memory Leak buildup (Count: %d)", state.ChaosCount)
					return
				case "NetworkDelay":
					simulateNetworkDelay(elapsed)
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, "Error: Server failed due to Network Delay buildup (Count: %d)", state.ChaosCount)
					return
				case "ResourceExhaustion":
					simulateResourceExhaustion(elapsed)
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, "Error: Server failed due to Resource Exhaustion buildup (Count: %d)", state.ChaosCount)
					return
				}
			}
		}
	}

	// If no chaos, return healthy response
	w.WriteHeader(http.StatusOK)
	if currentCrashType != "" {
		fmt.Fprintf(w, "Server is running... (Chaos Type: %s, Uptime: %v)", currentCrashType, time.Since(chaosStates[currentCrashType].BuildupStartTime))
	} else {
		fmt.Fprintf(w, "Server is running... (No chaos active)")
	}
}

func selectCrashType() {
	// Calculate total probability
	totalChance := config.HighCPULoadChance + config.MemoryLeakChance +
		config.NetworkDelayChance + config.ResourceExhaustionChance
	if totalChance <= 0 {
		return
	}

	// Chaos Monkey: Select one crash type
	r := rand.Float32() * totalChance
	if r < config.HighCPULoadChance {
		currentCrashType = "HighCPULoad"
	} else if r < (config.HighCPULoadChance + config.MemoryLeakChance) {
		currentCrashType = "MemoryLeak"
	} else if r < (config.HighCPULoadChance + config.MemoryLeakChance + config.NetworkDelayChance) {
		currentCrashType = "NetworkDelay"
	} else {
		currentCrashType = "ResourceExhaustion"
	}

	// Ensure the selected crash type starts fresh if it was reset
	if chaosStates[currentCrashType].ChaosCount == 0 {
		chaosStates[currentCrashType].BuildupStartTime = time.Now()
	}
	fmt.Printf("Chaos Monkey selected crash type: %s\n", currentCrashType)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if currentCrashType != "" {
		fmt.Fprintf(w, "Health check: OK (Chaos Type: %s, Uptime: %v)", currentCrashType, time.Since(chaosStates[currentCrashType].BuildupStartTime))
	} else {
		fmt.Fprintf(w, "Health check: OK (No chaos active)")
	}
}

func simulateHighCPU(elapsed time.Duration) {
	fmt.Printf("💥 High CPU Load simulated at %v\n", elapsed)
	iterations := int(float64(1e6) * (float64(elapsed.Seconds()) / float64(config.BuildupDuration.Seconds())))
	if iterations > 1e6 {
		iterations = 1e6 // Reduced to prevent laptop hanging
	}
	go func() {
		for i := 0; i < iterations; i++ {
			_ = rand.Int()
		}
	}()
}

func simulateMemoryLeak(elapsed time.Duration) {
	fmt.Printf("💥 Memory Leak simulated at %v\n", elapsed)
	mu.Lock()
	defer mu.Unlock()
	chunkSize := int(float64(1*1024*1024) * (float64(elapsed.Seconds()) / float64(config.BuildupDuration.Seconds())))
	if chunkSize > 1*1024*1024 {
		chunkSize = 1 * 1024 * 1024 // Reduced to 1MB to prevent laptop hanging
	}
	chunk := make([]byte, chunkSize)
	for i := range chunk {
		chunk[i] = byte(rand.Intn(256))
	}
	memoryHog = append(memoryHog, chunk...)
	runtime.GC()
}

func simulateNetworkDelay(elapsed time.Duration) {
	fmt.Printf("💥 Network Delay simulated at %v\n", elapsed)
	maxDelay := int(float64(500) * (float64(elapsed.Seconds()) / float64(config.BuildupDuration.Seconds())))
	if maxDelay > 500 {
		maxDelay = 500 // Reduced to 500ms to prevent excessive delays
	}
	time.Sleep(time.Duration(rand.Intn(maxDelay)) * time.Millisecond)
}

func simulateResourceExhaustion(elapsed time.Duration) {
	fmt.Printf("💥 Resource Exhaustion simulated at %v\n", elapsed)
	goroutineCount := int(float64(10) * (float64(elapsed.Seconds()) / float64(config.BuildupDuration.Seconds())))
	if goroutineCount > 10 {
		goroutineCount = 10 // Reduced to 10 goroutines to prevent laptop hanging
	}
	for i := 0; i < goroutineCount; i++ {
		go func() {
			time.Sleep(2 * time.Second) // Reduced sleep time
		}()
	}
}
