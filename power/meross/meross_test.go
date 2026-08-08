package meross

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testKey = "s3cr3t-account-key"

// fakeEnvelope is the shape of a request this package sends, as seen by a
// fake plug server.
type fakeEnvelope struct {
	Header struct {
		MessageID string `json:"messageId"`
		Namespace string `json:"namespace"`
		Method    string `json:"method"`
		Timestamp int64  `json:"timestamp"`
		Sign      string `json:"sign"`
	} `json:"header"`
	Payload json.RawMessage `json:"payload"`
}

// computeSign independently reproduces the protocol's signature algorithm,
// so tests aren't just checking the package agrees with itself.
func computeSign(messageID, key string, timestamp int64) string {
	sum := md5.Sum([]byte(messageID + key + strconv.FormatInt(timestamp, 10)))
	return hex.EncodeToString(sum[:])
}

// newFakeServer starts a fake plug that verifies the request signature
// against key (500ing on mismatch, exactly as real firmware would reject a
// bad signature) and otherwise delegates to handle.
func newFakeServer(t *testing.T, key string, handle func(w http.ResponseWriter, env fakeEnvelope)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var env fakeEnvelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if env.Header.Sign != computeSign(env.Header.MessageID, key, env.Header.Timestamp) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		handle(w, env)
	}))
}

// serverIP strips the scheme off an httptest.Server URL, giving the
// "host:port" form Client expects.
func serverIP(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

func writeACK(w http.ResponseWriter, method string, payload any) {
	body, _ := json.Marshal(map[string]any{
		"header":  map[string]any{"method": method + "ACK"},
		"payload": payload,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func TestSetPower(t *testing.T) {
	t.Run("sends correct namespace, payload and signature", func(t *testing.T) {
		var gotEnv fakeEnvelope
		srv := newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
			gotEnv = env
			writeACK(w, "SET", env.Payload)
		})
		defer srv.Close()

		c, err := NewClient(serverIP(srv), testKey)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.SetPower(true); err != nil {
			t.Fatalf("SetPower: %v", err)
		}

		if gotEnv.Header.Namespace != "Appliance.Control.ToggleX" {
			t.Errorf("namespace = %q, want Appliance.Control.ToggleX", gotEnv.Header.Namespace)
		}
		if gotEnv.Header.Method != "SET" {
			t.Errorf("method = %q, want SET", gotEnv.Header.Method)
		}

		var payload struct {
			ToggleX struct {
				Channel int `json:"channel"`
				Onoff   int `json:"onoff"`
			} `json:"togglex"`
		}
		if err := json.Unmarshal(gotEnv.Payload, &payload); err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		if payload.ToggleX.Channel != 0 || payload.ToggleX.Onoff != 1 {
			t.Errorf("payload.togglex = %+v, want channel 0, onoff 1", payload.ToggleX)
		}
	})

	t.Run("WithChannel plumbs through to the payload", func(t *testing.T) {
		var gotEnv fakeEnvelope
		srv := newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
			gotEnv = env
			writeACK(w, "SET", env.Payload)
		})
		defer srv.Close()

		c, err := NewClient(serverIP(srv), testKey, WithChannel(3))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.SetPower(false); err != nil {
			t.Fatalf("SetPower: %v", err)
		}

		var payload struct {
			ToggleX struct {
				Channel int `json:"channel"`
				Onoff   int `json:"onoff"`
			} `json:"togglex"`
		}
		if err := json.Unmarshal(gotEnv.Payload, &payload); err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		if payload.ToggleX.Channel != 3 || payload.ToggleX.Onoff != 0 {
			t.Errorf("payload.togglex = %+v, want channel 3, onoff 0", payload.ToggleX)
		}
	})

	t.Run("wrong key produces a rejected signature", func(t *testing.T) {
		srv := newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
			writeACK(w, "SET", env.Payload)
		})
		defer srv.Close()

		c, err := NewClient(serverIP(srv), "not-the-right-key")
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.SetPower(true); err == nil {
			t.Fatal("SetPower with wrong key: got nil error, want an error")
		}
	})

	t.Run("ERROR response is a hard error", func(t *testing.T) {
		srv := newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
			body, _ := json.Marshal(map[string]any{
				"header":  map[string]any{"method": "ERROR"},
				"payload": map[string]any{"error": map[string]any{"code": 5000}},
			})
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
		})
		defer srv.Close()

		c, err := NewClient(serverIP(srv), testKey)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.SetPower(true); err == nil {
			t.Fatal("SetPower against an ERROR response: got nil error, want an error")
		}
	})

	t.Run("connection refused is an error", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("net.Listen: %v", err)
		}
		addr := l.Addr().String()
		l.Close() // nothing is listening now, so the port refuses connections

		c, err := NewClient(addr, testKey, WithTimeout(500*time.Millisecond))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.SetPower(true); err == nil {
			t.Fatal("SetPower against a closed port: got nil error, want an error")
		}
	})

	t.Run("slow plug times out", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
		}))
		defer srv.Close()

		c, err := NewClient(serverIP(srv), testKey, WithTimeout(20*time.Millisecond))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if err := c.SetPower(true); err == nil {
			t.Fatal("SetPower against a slow plug: got nil error, want a timeout error")
		}
	})
}

var messageIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestMessageIDsAreHexAndUnique(t *testing.T) {
	seen := make(map[string]bool)
	srv := newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
		if !messageIDPattern.MatchString(env.Header.MessageID) {
			t.Errorf("messageId %q does not match ^[0-9a-f]{32}$", env.Header.MessageID)
		}
		if seen[env.Header.MessageID] {
			t.Errorf("messageId %q was reused across requests", env.Header.MessageID)
		}
		seen[env.Header.MessageID] = true
		writeACK(w, "SET", env.Payload)
	})
	defer srv.Close()

	c, err := NewClient(serverIP(srv), testKey)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := c.SetPower(i%2 == 0); err != nil {
			t.Fatalf("SetPower: %v", err)
		}
	}
	if len(seen) != 5 {
		t.Errorf("saw %d unique message ids, want 5", len(seen))
	}
}

const systemAllFixture = `{
	"all": {
		"system": {
			"hardware": {
				"type": "mss315",
				"macAddress": "AA:BB:CC:DD:EE:FF"
			}
		},
		"digest": {
			"togglex": [
				{"channel": 0, "onoff": 1}
			]
		}
	}
}`

func newSystemAllServer(t *testing.T) *httptest.Server {
	t.Helper()
	var raw json.RawMessage = []byte(systemAllFixture)
	return newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
		if env.Header.Namespace != "Appliance.System.All" || env.Header.Method != "GET" {
			t.Fatalf("unexpected request: namespace=%q method=%q", env.Header.Namespace, env.Header.Method)
		}
		writeACK(w, "GET", raw)
	})
}

func TestState(t *testing.T) {
	srv := newSystemAllServer(t)
	defer srv.Close()

	c, err := NewClient(serverIP(srv), testKey)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	on, err := c.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !on {
		t.Errorf("State() = false, want true")
	}
}

func TestModel(t *testing.T) {
	srv := newSystemAllServer(t)
	defer srv.Close()

	c, err := NewClient(serverIP(srv), testKey)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	model, mac, err := c.Model()
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	if model != "mss315" || mac != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("Model() = (%q, %q), want (\"mss315\", \"AA:BB:CC:DD:EE:FF\")", model, mac)
	}
}

func TestElectricity(t *testing.T) {
	const fixture = `{"electricity": {"channel": 0, "current": 1234, "voltage": 2305, "power": 45678}}`

	srv := newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
		if env.Header.Namespace != "Appliance.Control.Electricity" || env.Header.Method != "GET" {
			t.Fatalf("unexpected request: namespace=%q method=%q", env.Header.Namespace, env.Header.Method)
		}
		writeACK(w, "GET", json.RawMessage(fixture))
	})
	defer srv.Close()

	c, err := NewClient(serverIP(srv), testKey)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	got, err := c.Electricity()
	if err != nil {
		t.Fatalf("Electricity: %v", err)
	}

	want := Electricity{Volts: 230.5, Amps: 1.234, Watts: 45.678}
	if !approxEqual(got.Volts, want.Volts) || !approxEqual(got.Amps, want.Amps) || !approxEqual(got.Watts, want.Watts) {
		t.Errorf("Electricity() = %+v, want %+v", got, want)
	}
}

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestNewClientRequiresIP(t *testing.T) {
	if _, err := NewClient("", "key"); err == nil {
		t.Fatal("NewClient with empty ip: got nil error, want an error")
	}
}

func TestNewClientAllowsEmptyKey(t *testing.T) {
	srv := newFakeServer(t, "", func(w http.ResponseWriter, env fakeEnvelope) {
		writeACK(w, "SET", env.Payload)
	})
	defer srv.Close()

	c, err := NewClient(serverIP(srv), "")
	if err != nil {
		t.Fatalf("NewClient with empty key: %v", err)
	}
	if err := c.SetPower(true); err != nil {
		t.Fatalf("SetPower with empty key: %v", err)
	}
}

func TestNew_ReturnsWorkingPowerFunc(t *testing.T) {
	var gotOnoff int
	srv := newFakeServer(t, testKey, func(w http.ResponseWriter, env fakeEnvelope) {
		var payload struct {
			ToggleX struct {
				Onoff int `json:"onoff"`
			} `json:"togglex"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			t.Fatalf("decoding payload: %v", err)
		}
		gotOnoff = payload.ToggleX.Onoff
		writeACK(w, "SET", env.Payload)
	})
	defer srv.Close()

	fn, err := New(serverIP(srv), testKey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := fn(true); err != nil {
		t.Fatalf("PowerFunc(true): %v", err)
	}
	if gotOnoff != 1 {
		t.Errorf("onoff = %d, want 1", gotOnoff)
	}
}

// Ensure test helpers stay honest with themselves: computeSign must agree
// with a plain fmt-based reimplementation, catching any accidental drift.
func TestComputeSignSelfCheck(t *testing.T) {
	got := computeSign("abc", "key", 42)
	sum := md5.Sum([]byte(fmt.Sprintf("abc%s%d", "key", 42)))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("computeSign = %q, want %q", got, want)
	}
}
