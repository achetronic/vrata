// Package model defines the core domain types used throughout Rutoso.
package model

// Listener describes a network entry point where Rutoso accepts HTTP traffic.
type Listener struct {
	// ID is the unique identifier of the listener.
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable label for the listener.
	Name string `json:"name" yaml:"name"`

	// Address is the IP address the listener binds to.
	// Defaults to "0.0.0.0" if empty.
	Address string `json:"address,omitempty" yaml:"address,omitempty"`

	// Port is the TCP port the listener binds to.
	Port uint32 `json:"port" yaml:"port"`

	// TLS holds optional TLS termination configuration.
	// When nil, the listener operates in plaintext mode.
	TLS *ListenerTLS `json:"tls,omitempty" yaml:"tls,omitempty"`

	// AccessLog configures access logging for this listener.
	// When nil, no access logs are emitted.
	AccessLog *AccessLogConfig `json:"accessLog,omitempty" yaml:"accessLog,omitempty"`

	// HTTP2 enables HTTP/2 on this listener. Required for gRPC clients.
	// With TLS, Go enables HTTP/2 automatically. Without TLS (h2c), Rutoso
	// enables h2c upgrade support.
	HTTP2 bool `json:"http2,omitempty" yaml:"http2,omitempty"`

	// ServerName sets the "Server" response header.
	// When empty, no Server header is added.
	ServerName string `json:"serverName,omitempty" yaml:"serverName,omitempty"`

	// MaxRequestHeadersKB limits the total size of request headers in
	// kilobytes. Requests exceeding this limit receive a 431 response.
	// Default: 0 (no limit).
	MaxRequestHeadersKB uint32 `json:"maxRequestHeadersKB,omitempty" yaml:"maxRequestHeadersKB,omitempty"`

}

// ListenerTLS holds TLS termination parameters for a Listener.
type ListenerTLS struct {
	// CertPath is the path to the PEM-encoded TLS certificate file.
	CertPath string `json:"certPath,omitempty" yaml:"certPath,omitempty"`

	// KeyPath is the path to the PEM-encoded private key file.
	KeyPath string `json:"keyPath,omitempty" yaml:"keyPath,omitempty"`

	// MinVersion is the minimum TLS protocol version to accept.
	// Accepted values: "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3".
	// Defaults to "TLSv1_2" if empty.
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`

	// MaxVersion is the maximum TLS protocol version to accept.
	// Accepted values: same as MinVersion. If empty, no upper bound is set.
	MaxVersion string `json:"maxVersion,omitempty" yaml:"maxVersion,omitempty"`
}
