package netguard

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// DialOptions configures a transport whose dialer checks and then dials the same resolved
// addresses, so a DNS answer cannot be swapped between the check and the connection.
type DialOptions struct {
	AllowPrivate        bool
	FirstByte           time.Duration
	DialTimeout         time.Duration
	KeepAlive           time.Duration
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
}

func NewTransport(options DialOptions) *http.Transport {
	dialTimeout := options.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}
	keepAlive := options.KeepAlive
	if keepAlive <= 0 {
		keepAlive = 30 * time.Second
	}
	idle := options.IdleConnTimeout
	if idle <= 0 {
		idle = 90 * time.Second
	}
	perHost := options.MaxIdleConnsPerHost
	if perHost <= 0 {
		perHost = 32
	}
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive}
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: options.FirstByte,
		MaxIdleConnsPerHost:   perHost,
		IdleConnTimeout:       idle,
		ForceAttemptHTTP2:     true,
		Proxy:                 nil,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := Resolve(ctx, host, options.AllowPrivate)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, address := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.Unmap().String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	return transport
}

// Client is the small client used for operator-triggered discovery.
func Client(allowPrivate bool, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport:     NewTransport(DialOptions{AllowPrivate: allowPrivate}),
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
