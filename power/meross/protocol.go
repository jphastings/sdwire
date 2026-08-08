package meross

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// requestHeader is the "header" object of a Meross local-API request.
type requestHeader struct {
	MessageID      string `json:"messageId"`
	Namespace      string `json:"namespace"`
	Method         string `json:"method"`
	PayloadVersion int    `json:"payloadVersion"`
	From           string `json:"from"`
	Timestamp      int64  `json:"timestamp"`
	Sign           string `json:"sign"`
	TriggerSrc     string `json:"triggerSrc"`
}

// requestEnvelope is the full body posted to the plug's /config endpoint.
type requestEnvelope struct {
	Header  requestHeader `json:"header"`
	Payload any           `json:"payload"`
}

// responseHeader is the subset of the response "header" object this package
// needs: the method name tells us whether the request succeeded (an "ACK"
// suffix) or failed (e.g. "ERROR").
type responseHeader struct {
	Method string `json:"method"`
}

// responseEnvelope is the full body returned by the plug's /config
// endpoint. Payload is left raw so each request can decode only the shape
// it expects.
type responseEnvelope struct {
	Header  responseHeader  `json:"header"`
	Payload json.RawMessage `json:"payload"`
}

// emptyPayload marshals to "{}", used for GET requests that don't need to
// send any request-specific data.
type emptyPayload struct{}

// endpoint returns the URL of the plug's local-API config endpoint.
func (c *Client) endpoint() string {
	return fmt.Sprintf("http://%s/config", c.ip)
}

// do performs a single request/response round trip against the plug and
// returns the raw response payload. It returns an error for any transport
// failure, non-200 HTTP status, or response whose header.method isn't the
// expected "<method>ACK" (this includes header.method == "ERROR") — the
// caller must never assume the requested change took effect unless do
// returns a nil error.
func (c *Client) do(method, namespace string, payload any) (json.RawMessage, error) {
	messageID, err := newMessageID()
	if err != nil {
		return nil, fmt.Errorf("meross: generating message id: %w", err)
	}
	timestamp := time.Now().Unix()

	env := requestEnvelope{
		Header: requestHeader{
			MessageID:      messageID,
			Namespace:      namespace,
			Method:         method,
			PayloadVersion: 1,
			From:           c.endpoint(),
			Timestamp:      timestamp,
			Sign:           sign(messageID, c.key, timestamp),
			TriggerSrc:     "Android",
		},
		Payload: payload,
	}

	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("meross: encoding %s %s request: %w", method, namespace, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("meross: building %s %s request: %w", method, namespace, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("meross: %s %s: %w", method, namespace, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("meross: %s %s: unexpected HTTP status %s", method, namespace, resp.Status)
	}

	var respEnv responseEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&respEnv); err != nil {
		return nil, fmt.Errorf("meross: %s %s: decoding response: %w", method, namespace, err)
	}

	wantMethod := method + "ACK"
	if respEnv.Header.Method != wantMethod {
		return nil, fmt.Errorf("meross: %s %s: plug returned method %q (want %q), payload: %s", method, namespace, respEnv.Header.Method, wantMethod, respEnv.Payload)
	}

	return respEnv.Payload, nil
}

// newMessageID generates the 32 lowercase hex character messageId the
// protocol requires, unique per request.
func newMessageID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// sign computes the request signature: hex(md5(messageId + key +
// timestamp)), with timestamp concatenated as its decimal string.
func sign(messageID, key string, timestamp int64) string {
	sum := md5.Sum([]byte(messageID + key + strconv.FormatInt(timestamp, 10)))
	return hex.EncodeToString(sum[:])
}
