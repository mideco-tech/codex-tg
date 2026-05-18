package controlapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Backend interface {
	Doctor(ctx context.Context) (map[string]any, error)
	ControlThreadList(ctx context.Context, limit int, cursor string) (map[string]any, error)
	ControlThreadRead(ctx context.Context, threadID string, includeTurns bool) (map[string]any, error)
}

func NewHandler(version string, backend Backend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"version": version,
		})
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		status, err := backend.Doctor(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/v1/threads", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/threads" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		limit := parseLimit(r.URL.Query().Get("limit"), 20, 100)
		cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
		payload, err := backend.ControlThreadList(r.Context(), limit, cursor)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, payload)
	})
	mux.HandleFunc("/v1/threads/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		threadID, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/v1/threads/"))
		if err != nil || strings.TrimSpace(threadID) == "" || strings.Contains(threadID, "/") {
			writeError(w, http.StatusBadRequest, "valid thread id is required")
			return
		}
		includeTurns, err := parseOptionalBool(r.URL.Query().Get("include_turns"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "include_turns must be true or false")
			return
		}
		payload, err := backend.ControlThreadRead(r.Context(), threadID, includeTurns)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, payload)
	})
	return mux
}

func Listen(address string) (net.Listener, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, nil
	}
	if err := validateLoopbackTCP(address); err != nil {
		return nil, err
	}
	return net.Listen("tcp", address)
}

func Serve(ctx context.Context, listener net.Listener, handler http.Handler, logger *log.Logger) error {
	if listener == nil {
		return nil
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && logger != nil {
			logger.Printf("control api shutdown failed: %v", err)
		}
	}()
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func validateLoopbackTCP(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("control api listen must be host:port on loopback: %w", err)
	}
	if strings.TrimSpace(port) == "" {
		return errors.New("control api listen port is required")
	}
	if !isLoopbackHost(host) {
		return errors.New("control api listen must use 127.0.0.1, ::1, or localhost")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parseLimit(raw string, fallback, max int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > max {
		return max
	}
	return parsed
}

func parseOptionalBool(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
