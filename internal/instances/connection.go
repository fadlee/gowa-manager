package instances

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultConnectionTimeout = 3 * time.Second

type ConnectionTesterOptions struct {
	HTTPClient *http.Client
	Timeout    time.Duration
}

type ConnectionTester struct {
	httpClient *http.Client
	timeout    time.Duration
}

type ConnectionTestResult struct {
	OK      bool   `json:"ok"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message"`
	Body    string `json:"body,omitempty"`
}

func NewConnectionTester(options ConnectionTesterOptions) *ConnectionTester {
	client := options.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultConnectionTimeout
	}
	return &ConnectionTester{httpClient: client, timeout: timeout}
}

func (t *ConnectionTester) Test(ctx context.Context, instance Instance) ConnectionTestResult {
	if !isRunningWithPort(instance) {
		return ConnectionTestResult{Message: "Instance is not running. Start it before testing the GOWA API connection."}
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, devicesURL(instance), nil)
	if err != nil {
		return ConnectionTestResult{Message: sanitizeConnectionError(err.Error(), instance)}
	}
	applyCommonHeaders(req, instance)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		message := err.Error()
		if message == "" {
			message = "Connection failed before receiving a response."
		}
		return ConnectionTestResult{Message: sanitizeConnectionError(message, instance)}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 601))
	bodyText := string(body)
	if bodyText == "" {
		bodyText = "No response body."
	} else if len(bodyText) > 600 {
		bodyText = bodyText[:600] + "..."
	}
	bodyText = sanitizeConnectionError(bodyText, instance)
	message := "Failed to connect to the GOWA API."
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		message = "Successfully connected to the GOWA API."
	}
	return ConnectionTestResult{OK: resp.StatusCode >= 200 && resp.StatusCode <= 299, Status: resp.StatusCode, Message: message, Body: bodyText}
}

func sanitizeConnectionError(message string, instance Instance) string {
	config := ParseConfig(instance.Config)
	for _, auth := range config.Flags.BasicAuth {
		if auth.Username != "" || auth.Password != "" {
			message = strings.ReplaceAll(message, "Basic "+basicAuthToken(auth.Username, auth.Password), "Basic [redacted]")
		}
		if auth.Username != "" {
			message = strings.ReplaceAll(message, auth.Username, "[redacted]")
		}
		if auth.Password != "" {
			message = strings.ReplaceAll(message, auth.Password, "[redacted]")
		}
	}
	if message == "" {
		return "Connection failed before receiving a response."
	}
	return message
}

func basicAuthToken(username string, password string) string {
	req, err := http.NewRequest(http.MethodGet, "http://localhost", nil)
	if err != nil {
		return ""
	}
	req.SetBasicAuth(username, password)
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Basic ")
}
