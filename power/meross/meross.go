// Package meross drives Meross smart plugs (MSS315 and compatible models)
// over their local HTTP API, with no dependency on the Meross cloud at
// runtime. It exists to provide an sdwire.PowerFunc that switches a target
// board's mains power via a Meross plug — see New.
package meross

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jphastings/sdwire"
)

// DefaultTimeout is the HTTP timeout used when no WithTimeout option is given.
const DefaultTimeout = 5 * time.Second

// Client talks to a single Meross plug's local HTTP API.
type Client struct {
	ip         string
	key        string
	channel    int
	httpClient *http.Client
	timeout    time.Duration
}

// Option configures a Client constructed by NewClient.
type Option func(*Client)

// WithTimeout sets the per-request timeout, overriding DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithChannel selects the relay channel used by SetPower and State, for
// multi-outlet Meross devices. It defaults to 0, the only channel on
// single-outlet plugs like the MSS315.
func WithChannel(channel int) Option {
	return func(c *Client) { c.channel = channel }
}

// WithHTTPClient overrides the *http.Client used to reach the plug, e.g. to
// customize transport behaviour. The per-request timeout set by
// WithTimeout (or DefaultTimeout) is still applied via request context,
// independently of any Timeout configured on this client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient constructs a Client for the plug at ip, authenticating requests
// with key. ip must not be empty. key is the Meross *account* key shared by
// every plug on that account (see the package README for how to obtain
// it); very old plug firmware accepts an empty key, so that is worth
// trying before fetching real credentials.
func NewClient(ip, key string, opts ...Option) (*Client, error) {
	if ip == "" {
		return nil, errors.New("meross: ip must not be empty")
	}

	c := &Client{
		ip:         ip,
		key:        key,
		httpClient: &http.Client{},
		timeout:    DefaultTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// New constructs a Client and returns its relay switch as an
// sdwire.PowerFunc, ready to pass to sdwire.WithTargetPower or
// SDWire.SetTargetPower. See Client.SetPower for behaviour and caveats.
func New(ip, key string, opts ...Option) (sdwire.PowerFunc, error) {
	c, err := NewClient(ip, key, opts...)
	if err != nil {
		return nil, err
	}
	return c.PowerFunc(), nil
}

// PowerFunc returns c.SetPower as an sdwire.PowerFunc.
func (c *Client) PowerFunc() sdwire.PowerFunc {
	return c.SetPower
}

// SetPower switches the plug's relay on (on=true) or off. It blocks until
// the plug acknowledges the change and returns an error for any transport
// failure, non-200 response, or non-acknowledging reply (including an
// explicit header.method == "ERROR") — the relay must never be assumed to
// have switched unless SetPower returns nil.
//
// Turning the relay off is not the same as the target board losing power
// instantly: the plug's own control electronics stay powered even with the
// relay open (so the plug remains reachable), and small target boards can
// themselves ride through a brief interruption on PSU bulk capacitance.
// Callers that need a guaranteed power cycle — e.g. sdwire.SDWire.PowerCycle
// — are responsible for holding power off for a sufficient minimum dark
// time.
func (c *Client) SetPower(on bool) error {
	onoff := 0
	if on {
		onoff = 1
	}

	payload := map[string]any{
		"togglex": map[string]any{
			"channel": c.channel,
			"onoff":   onoff,
		},
	}

	if _, err := c.do("SET", "Appliance.Control.ToggleX", payload); err != nil {
		return fmt.Errorf("meross: setting power: %w", err)
	}
	return nil
}

// systemAllPayload is the payload of an Appliance.System.All GETACK.
type systemAllPayload struct {
	All struct {
		System struct {
			Hardware struct {
				Type       string `json:"type"`
				MacAddress string `json:"macAddress"`
			} `json:"hardware"`
		} `json:"system"`
		Digest struct {
			ToggleX []struct {
				Channel int `json:"channel"`
				Onoff   int `json:"onoff"`
			} `json:"togglex"`
		} `json:"digest"`
	} `json:"all"`
}

func (c *Client) systemAll() (systemAllPayload, error) {
	raw, err := c.do("GET", "Appliance.System.All", emptyPayload{})
	if err != nil {
		return systemAllPayload{}, err
	}

	var parsed systemAllPayload
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return systemAllPayload{}, fmt.Errorf("meross: parsing Appliance.System.All payload: %w", err)
	}
	return parsed, nil
}

// State reports whether the plug's relay (on the configured channel, see
// WithChannel) is currently on.
func (c *Client) State() (bool, error) {
	parsed, err := c.systemAll()
	if err != nil {
		return false, fmt.Errorf("meross: reading state: %w", err)
	}

	for _, tx := range parsed.All.Digest.ToggleX {
		if tx.Channel == c.channel {
			return tx.Onoff != 0, nil
		}
	}
	return false, fmt.Errorf("meross: reading state: no togglex entry for channel %d", c.channel)
}

// Model returns the plug's hardware type (e.g. "mss315") and MAC address.
func (c *Client) Model() (model, mac string, err error) {
	parsed, err := c.systemAll()
	if err != nil {
		return "", "", fmt.Errorf("meross: reading model: %w", err)
	}
	return parsed.All.System.Hardware.Type, parsed.All.System.Hardware.MacAddress, nil
}

// Electricity is a live metering reading from a plug.
type Electricity struct {
	Volts float64
	Amps  float64
	Watts float64
}

// electricityPayload is the payload of an Appliance.Control.Electricity GETACK.
type electricityPayload struct {
	Electricity struct {
		Channel int `json:"channel"`
		Current int `json:"current"` // milliamps
		Voltage int `json:"voltage"` // decivolts (tenths of a volt)
		Power   int `json:"power"`   // milliwatts
	} `json:"electricity"`
}

// Electricity reads live metering from the plug. Not all Meross models
// support metering — the MSS315 does; on models that don't, this returns
// the plug's own error.
//
// Readings lag the true instantaneous values by several seconds inside the
// plug's firmware: treat any single reading as noisy, and trust a trend
// across repeated calls rather than one instant value.
func (c *Client) Electricity() (Electricity, error) {
	raw, err := c.do("GET", "Appliance.Control.Electricity", emptyPayload{})
	if err != nil {
		return Electricity{}, fmt.Errorf("meross: reading electricity: %w", err)
	}

	var parsed electricityPayload
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Electricity{}, fmt.Errorf("meross: parsing Appliance.Control.Electricity payload: %w", err)
	}

	return Electricity{
		Volts: float64(parsed.Electricity.Voltage) / 10,
		Amps:  float64(parsed.Electricity.Current) / 1000,
		Watts: float64(parsed.Electricity.Power) / 1000,
	}, nil
}
