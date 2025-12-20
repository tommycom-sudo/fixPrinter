package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Server represents a lightweight reverse proxy that rewrites headers to allow embedding.
type Server struct {
	target   *url.URL
	listener net.Listener
	server   *http.Server
	baseURL  string
}

// New creates a reverse proxy server targeting the given backend (e.g., http://172.20.38.62:8080).
func New(targetBase string) (*Server, error) {
	parsed, err := url.Parse(targetBase)
	if err != nil {
		return nil, fmt.Errorf("parse proxy target: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid proxy target %q", targetBase)
	}

	return &Server{
		target: parsed,
	}, nil
}

// Start launches the proxy on a random localhost port and returns the base URL.
func (s *Server) Start() (string, error) {
	if s.listener != nil {
		return s.baseURL, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("start proxy listener: %w", err)
	}
	s.listener = listener

	proxy := httputil.NewSingleHostReverseProxy(s.target)
	defaultDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		defaultDirector(r)
		r.Host = s.target.Host
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		return nil
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Scheme = s.target.Scheme
		r.URL.Host = s.target.Host
		r.Host = s.target.Host
		proxy.ServeHTTP(w, r)
	})

	s.server = &http.Server{
		Handler: handler,
	}

	s.baseURL = fmt.Sprintf("http://%s", listener.Addr().String())
	go s.server.Serve(listener) // nolint:errcheck

	return s.baseURL, nil
}

// BaseURL returns the proxied origin exposed to the UI.
func (s *Server) BaseURL() string {
	return s.baseURL
}

// Rewrite swaps the target base with the current proxy base.
func (s *Server) Rewrite(raw string) string {
	if s.baseURL == "" || raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, s.target.String()) {
		return s.baseURL + strings.TrimPrefix(raw, s.target.String())
	}
	return raw
}

// Stop gracefully shuts down the proxy.
func (s *Server) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}
