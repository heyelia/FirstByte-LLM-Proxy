package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	openAPIBaseURL = "https://api.openai.com"
	requestCount   = 2
	bufferSize     = 8192
)

var (
	httpClient = &http.Client{
		Transport: &http.Transport{
			DisableCompression: false,
			MaxIdleConns:      100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:   90 * time.Second,
			ForceAttemptHTTP2: true,
			MaxConnsPerHost:   100,
		},
		Timeout: 60 * time.Second,
	}
	openAIAPIKey string

	bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, bufferSize)
		},
	}
)

type requestBody struct {
	Stream bool `json:"stream"`
}

type responseWrapper struct {
	response *http.Response
	err      error
	body     []byte
	latency  time.Duration
}

func main() {
	openAIAPIKey = os.Getenv("OPENAI_API_KEY")
	if openAIAPIKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable not set.")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      nil,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	http.HandleFunc("/hc", healthCheckHandler)
	http.HandleFunc("/v1/", proxyHandler)

	log.Printf("Go OpenAI Proxy server starting on port %s", port)
	log.Printf("Will make %d requests to OpenAI API at %s", requestCount, openAPIBaseURL)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	var originalBodyBytes []byte
	if r.Body != nil {
		var err error
		originalBodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusInternalServerError)
			return
		}
		r.Body.Close()
	}

	var reqBodyData requestBody
	isStreaming := false
	if len(originalBodyBytes) > 0 {
		if err := json.Unmarshal(originalBodyBytes, &reqBodyData); err == nil {
			isStreaming = reqBodyData.Stream
		} else {
			if strings.Contains(string(originalBodyBytes), `"stream":true`) {
				isStreaming = true
			}
			log.Printf("Warning: Could not unmarshal request body to check for stream: %v. Assuming %v based on string check.", err, isStreaming)
		}
	}

	targetURL := openAPIBaseURL + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	log.Printf("Proxying %s %s to %s (isStreaming: %t)", r.Method, r.URL.Path, targetURL, isStreaming)

	if isStreaming {
		handleStreamingRequest(w, r, originalBodyBytes, targetURL, startTime)
	} else {
		handleNonStreamingRequest(w, r, originalBodyBytes, targetURL, startTime)
	}
}

func handleStreamingRequest(w http.ResponseWriter, r *http.Request, originalBodyBytes []byte, targetURL string, startTime time.Time) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	firstResponseStarted := make(chan struct{}, 1)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	headersMu := sync.Mutex{}
	headersSet := false

	var wg sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func(requestNum int) {
			defer wg.Done()
			reqStartTime := time.Now()

			proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(originalBodyBytes))
			if err != nil {
				log.Printf("Failed to create proxy request %d: %v", requestNum, err)
				return
			}

			copyHeaders(r.Header, proxyReq.Header)
			proxyReq.Header.Set("Authorization", "Bearer "+openAIAPIKey)
			proxyReq.Header.Set("User-Agent", "Go-OpenAI-Proxy/1.0")
			proxyReq.Header.Del("Content-Length")

			resp, err := httpClient.Do(proxyReq)
			latency := time.Since(reqStartTime)
			if err != nil {
				log.Printf("Error from OpenAI API on streaming request %d (latency: %s): %v", requestNum, latency, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				log.Printf("Request %d received non-2xx status %d (latency: %s)", requestNum, resp.StatusCode, latency)
				return
			}

			headersMu.Lock()
			if !headersSet {
				copyHeaders(resp.Header, w.Header())

				w.Header().Del("Content-Length")

				if w.Header().Get("Content-Type") == "" {
					w.Header().Set("Content-Type", "text/event-stream")
				}

				w.WriteHeader(resp.StatusCode)
				headersSet = true

				select {
				case firstResponseStarted <- struct{}{}:
				default:
				}

				headersMu.Unlock()

				buf := bufferPool.Get().([]byte)
				defer bufferPool.Put(buf)

				for {
					select {
					case <-ctx.Done():
						return
					default:
					}

					n, err := resp.Body.Read(buf)
					if n > 0 {
						_, writeErr := w.Write(buf[:n])
						if writeErr != nil {
							log.Printf("Error writing chunk to client: %v", writeErr)
							cancel()
							return
						}
						flusher.Flush()
					}

					if err == io.EOF {
						break
					}
					if err != nil {
						log.Printf("Error reading stream from OpenAI in request %d: %v", requestNum, err)
						break
					}
				}

				totalLatency := time.Since(startTime)
				log.Printf("Successfully streamed response %d. Total latency: %s, OpenAI request latency: %s",
					requestNum, totalLatency, latency)

				cancel()
			} else {
				headersMu.Unlock()

				select {
				case <-ctx.Done():
					return
				case <-firstResponseStarted:
					return
				}
			}
		}(i)
	}

	wg.Wait()
}

func handleNonStreamingRequest(w http.ResponseWriter, r *http.Request, originalBodyBytes []byte, targetURL string, startTime time.Time) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	responseChan := make(chan responseWrapper, requestCount)
	var wg sync.WaitGroup

	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func(requestNum int) {
			defer wg.Done()
			reqStartTime := time.Now()

			proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, bytes.NewReader(originalBodyBytes))
			if err != nil {
				responseChan <- responseWrapper{err: fmt.Errorf("failed to create proxy request %d: %v", requestNum, err)}
				return
			}

			copyHeaders(r.Header, proxyReq.Header)
			proxyReq.Header.Set("Authorization", "Bearer "+openAIAPIKey)
			proxyReq.Header.Set("User-Agent", "Go-OpenAI-Proxy/1.0")
			proxyReq.Header.Del("Content-Length")

			resp, err := httpClient.Do(proxyReq)
			latency := time.Since(reqStartTime)

			if err != nil {
				log.Printf("Error from OpenAI API on request %d (latency: %s): %v", requestNum, latency, err)
				responseChan <- responseWrapper{err: fmt.Errorf("request %d to OpenAI API failed: %v", requestNum, err), latency: latency}
				return
			}

			bodyBytes, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				log.Printf("Error reading non-streaming response body for request %d (latency: %s): %v", requestNum, latency, readErr)
				responseChan <- responseWrapper{err: fmt.Errorf("request %d to OpenAI API failed reading body: %v", requestNum, readErr), latency: latency}
				return
			}

			log.Printf("Request %d (non-streaming) to %s completed with status %d in %s, body length: %d", requestNum, targetURL, resp.StatusCode, latency, len(bodyBytes))
			responseChan <- responseWrapper{response: resp, err: nil, body: bodyBytes, latency: latency}
		}(i)
	}

	go func() {
		wg.Wait()
		close(responseChan)
	}()

	var firstSuccessfulResponse *responseWrapper
	var receivedResponses []responseWrapper

	completedRequests := 0
	for res := range responseChan {
		completedRequests++
		receivedResponses = append(receivedResponses, res)
		if res.err == nil && res.response != nil && res.response.StatusCode >= 200 && res.response.StatusCode < 300 {
			if firstSuccessfulResponse == nil {
				log.Printf("Selected response with latency %s, status %d", res.latency, res.response.StatusCode)
				tempRes := res
				firstSuccessfulResponse = &tempRes

				cancel()
			}
		}

		if firstSuccessfulResponse != nil && completedRequests == requestCount {
			break
		}
	}

	if firstSuccessfulResponse == nil {
		log.Printf("No successful response from OpenAI API after %d attempts.", requestCount)
		http.Error(w, "All OpenAI requests failed.", http.StatusServiceUnavailable)
		return
	}

	chosenResp := firstSuccessfulResponse.response

	copyHeaders(chosenResp.Header, w.Header())

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	w.WriteHeader(chosenResp.StatusCode)
	w.Write(firstSuccessfulResponse.body)

	totalProxyLatency := time.Since(startTime)
	log.Printf("Successfully proxied %s. Chosen response status: %d. Total proxy latency: %s. OpenAI request latency: %s",
		r.URL.Path, chosenResp.StatusCode, totalProxyLatency, firstSuccessfulResponse.latency)
}

func copyHeaders(src, dst http.Header) {
	for key, values := range src {
		if strings.ToLower(key) == "content-length" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
