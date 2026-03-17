package model

// AccessLogConfig configures access logging. Attached to routes/groups
// as a middleware (type "accessLog").
type AccessLogConfig struct {
	// Path is the file path where access logs are written.
	// Use "/dev/stdout" or "stdout" for container-friendly stdout logging.
	Path string `json:"path" yaml:"path"`

	// JSON outputs each log entry as a JSON object. When false, outputs
	// as key=value pairs on a single line.
	JSON bool `json:"json,omitempty" yaml:"json,omitempty"`

	// OnRequest defines the fields logged when a request arrives.
	// If nil, no request log is emitted.
	OnRequest *AccessLogEntry `json:"onRequest,omitempty" yaml:"onRequest,omitempty"`

	// OnResponse defines the fields logged when the response is sent.
	// If nil, no response log is emitted.
	OnResponse *AccessLogEntry `json:"onResponse,omitempty" yaml:"onResponse,omitempty"`
}

// AccessLogEntry defines which fields appear in a log line.
// Keys are the field names in the output. Values are templates with
// interpolation support:
//
//   ${id}                    — auto-generated UUID, same for request and response
//   ${request.method}        — HTTP method
//   ${request.path}          — URL path
//   ${request.host}          — hostname without port
//   ${request.authority}     — full Host header
//   ${request.scheme}        — http or https
//   ${request.clientIp}      — client IP (respects X-Forwarded-For)
//   ${request.header.NAME}   — any request header
//   ${response.status}       — HTTP status code
//   ${response.bytes}        — bytes written to client
//   ${response.header.NAME}  — any response header
//   ${duration.ms}           — request duration in milliseconds
//   ${duration.us}           — request duration in microseconds
//   ${duration.s}            — request duration in seconds
type AccessLogEntry struct {
	Fields map[string]string `json:"fields" yaml:"fields"`
}
