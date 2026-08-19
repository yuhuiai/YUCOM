// Shared application, HTTP API and UI logic for YUCOM.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"yucom/internal/serialcore"
)

//go:embed web/index.html
var webFiles embed.FS

// CRTSCTS is defined by Linux asm-generic/termbits.h but is not exported by
// Go's syscall package on every architecture (including linux/arm64).
type serialConfig = serialcore.SerialConfig
type sendRequest = serialcore.SendRequest

type loopbackRequest struct {
	PayloadLength int `json:"payloadLength"`
	TimeoutMS     int `json:"timeoutMs"`
}

type loopbackResult struct {
	Passed        bool   `json:"passed"`
	SentBytes     int    `json:"sentBytes"`
	ReceivedBytes int    `json:"receivedBytes"`
	ElapsedMS     int64  `json:"elapsedMs"`
	Message       string `json:"message"`
}

type serialEvent struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Text string `json:"text,omitempty"`
}

type serialStatus struct {
	Opened    bool              `json:"opened"`
	Config    serialConfig      `json:"config"`
	RXCount   uint64            `json:"rxCount"`
	TXCount   uint64            `json:"txCount"`
	LastError string            `json:"lastError,omitempty"`
	Modem     map[string]bool   `json:"modem"`
	StartedAt string            `json:"startedAt,omitempty"`
	Version   string            `json:"version"`
	Extra     map[string]string `json:"extra,omitempty"`
}

type serialApp struct {
	mu          sync.RWMutex
	testMu      sync.Mutex
	file        *os.File
	config      serialConfig
	generation  uint64
	rxCount     uint64
	txCount     uint64
	lastError   string
	startedAt   time.Time
	subs        map[chan serialEvent]struct{}
	shutdown    chan struct{}
	testCapture serialcore.Capture
	testDone    chan struct{}
}

func newSerialApp() *serialApp {
	return &serialApp{
		subs:     make(map[chan serialEvent]struct{}),
		shutdown: make(chan struct{}, 1),
	}
}

func main() {
	runApp()
}

func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || (host != "127.0.0.1" && host != "::1") {
			http.Error(w, "仅允许本机访问", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (a *serialApp) routes(mux *http.ServeMux) {
	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(webRoot))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, readErr := webFiles.ReadFile("web/index.html")
		if readErr != nil {
			http.Error(w, readErr.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/api/ports", a.handlePorts)
	mux.HandleFunc("/api/open", a.handleOpen)
	mux.HandleFunc("/api/close", a.handleClose)
	mux.HandleFunc("/api/send", a.handleSend)
	mux.HandleFunc("/api/loopback-test", a.handleLoopbackTest)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/events", a.handleEvents)
	mux.HandleFunc("/api/reset-counters", a.handleResetCounters)
	mux.HandleFunc("/api/shutdown", a.handleShutdown)
}

type portInfo struct {
	Device string `json:"device"`
	Group  string `json:"group"`
}

func (a *serialApp) handlePorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ports": enumeratePorts()})
}

func (a *serialApp) handleOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var cfg serialConfig
	if err := decodeJSON(r, &cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if err := validateConfig(cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.openPort(cfg); err != nil {
		writeAPIError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *serialApp) handleClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	a.closePort()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *serialApp) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req sendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	payload, err := serialcore.PreparePayload(req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	n, err := a.send(payload)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bytes": n})
}

func (a *serialApp) handleLoopbackTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req loopbackRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if req.PayloadLength < 32 || req.PayloadLength > 4096 {
		writeAPIError(w, http.StatusBadRequest, errors.New("回环数据长度必须在32～4096字节之间"))
		return
	}
	if req.TimeoutMS < 200 || req.TimeoutMS > 10000 {
		writeAPIError(w, http.StatusBadRequest, errors.New("回环超时时间必须在200～10000ms之间"))
		return
	}
	result, err := a.loopbackTest(req.PayloadLength, time.Duration(req.TimeoutMS)*time.Millisecond)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *serialApp) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, a.status())
}

func (a *serialApp) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "浏览器不支持实时数据", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan serialEvent, 128)
	a.mu.Lock()
	a.subs[ch] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.subs, ch)
		a.mu.Unlock()
	}()

	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case event := <-ch:
			encoded, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (a *serialApp) handleResetCounters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	a.resetCounters()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// resetCounters clears only the software byte counters. It deliberately does
// not close or reconfigure the active serial port and does not affect data on
// the wire.
func (a *serialApp) resetCounters() {
	a.mu.Lock()
	a.rxCount = 0
	a.txCount = 0
	a.mu.Unlock()
}

func (a *serialApp) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	select {
	case a.shutdown <- struct{}{}:
	default:
	}
}
func (a *serialApp) loopbackTest(payloadLength int, timeout time.Duration) (loopbackResult, error) {
	a.testMu.Lock()
	defer a.testMu.Unlock()

	a.mu.RLock()
	file := a.file
	a.mu.RUnlock()
	if file == nil {
		return loopbackResult{}, errors.New("请先打开要测试的串口")
	}

	payload := serialcore.MakeLoopbackPayload(payloadLength)
	done := make(chan struct{})
	a.mu.Lock()
	if a.file != file {
		a.mu.Unlock()
		return loopbackResult{}, errors.New("串口状态已经变化，请重新测试")
	}
	a.testCapture.Start(payload)
	a.testDone = done
	a.mu.Unlock()

	started := time.Now()
	sent, sendErr := a.send(payload)
	if sendErr != nil {
		a.clearLoopbackCapture()
		return loopbackResult{}, sendErr
	}

	select {
	case <-done:
	case <-time.After(timeout):
	}

	a.mu.Lock()
	passed := a.testCapture.Matched
	received := len(a.testCapture.Received)
	a.testCapture.Reset()
	a.testDone = nil
	a.mu.Unlock()

	result := loopbackResult{
		Passed:        passed,
		SentBytes:     sent,
		ReceivedBytes: received,
		ElapsedMS:     time.Since(started).Milliseconds(),
	}
	if passed {
		result.Message = "回环数据完全一致，串口收发通道通过"
	} else {
		result.Message = "超时或数据不一致；请检查回环接线、电气标准和收发方向"
	}
	return result, nil
}

func (a *serialApp) clearLoopbackCapture() {
	a.mu.Lock()
	a.testCapture.Reset()
	a.testDone = nil
	a.mu.Unlock()
}

func (a *serialApp) send(payload []byte) (int, error) {
	a.mu.RLock()
	file := a.file
	a.mu.RUnlock()
	if file == nil {
		return 0, errors.New("请先打开串口")
	}
	if len(payload) == 0 {
		return 0, errors.New("发送内容不能为空")
	}
	n, err := serialcore.WriteAll(file, payload)
	a.mu.Lock()
	a.txCount += uint64(n)
	if err != nil {
		a.lastError = err.Error()
		a.mu.Unlock()
		return n, fmt.Errorf("发送失败：%w", err)
	}
	a.mu.Unlock()
	return n, nil
}

func (a *serialApp) publish(event serialEvent) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for subscriber := range a.subs {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (a *serialApp) status() serialStatus {
	a.mu.RLock()
	file := a.file
	status := serialStatus{
		Opened:    file != nil,
		Config:    a.config,
		RXCount:   a.rxCount,
		TXCount:   a.txCount,
		LastError: a.lastError,
		Modem:     map[string]bool{"CTS": false, "DSR": false, "DCD": false, "RI": false, "RTS": false, "DTR": false},
		Version:   "1.2.0",
	}
	if !a.startedAt.IsZero() {
		status.StartedAt = a.startedAt.Format(time.RFC3339)
	}
	a.mu.RUnlock()
	if file != nil {
		status.Modem = modemSignals(file.Fd())
	}
	return status
}

func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求内容无效：%w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("请求内容无效：JSON后存在多余内容")
	}
	return nil
}

func methodNotAllowed(w http.ResponseWriter) {
	writeAPIError(w, http.StatusMethodNotAllowed, errors.New("请求方法不允许"))
}

func writeAPIError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
