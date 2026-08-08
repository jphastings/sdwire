package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jphastings/sdwire"
	"github.com/jphastings/sdwire/power/meross"
)

// PowerFactory builds an sdwire.PowerFunc from a device's "power:" config
// section (a map straight from YAML, including its "type" key). The SDK
// itself stays plugin-agnostic; this registry is what wires config-driven
// power plugins into the CLI.
type PowerFactory func(config map[string]any) (sdwire.PowerFunc, error)

// powerRegistry maps a power.type string to the factory that builds it.
var powerRegistry = map[string]PowerFactory{
	"meross": merossPowerFactory,
}

// registeredPowerTypes lists the registry's keys, sorted for stable output.
func registeredPowerTypes() []string {
	types := make([]string, 0, len(powerRegistry))
	for t := range powerRegistry {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// buildPowerFunc looks up config["type"] in powerRegistry and builds a
// PowerFunc from it.
func buildPowerFunc(config map[string]any) (sdwire.PowerFunc, error) {
	typ, _ := config["type"].(string)
	if typ == "" {
		return nil, fmt.Errorf("power config is missing \"type\"")
	}
	factory, ok := powerRegistry[typ]
	if !ok {
		return nil, fmt.Errorf("unknown power plugin type %q; registered types: %s", typ, strings.Join(registeredPowerTypes(), ", "))
	}
	return factory(config)
}

// merossPowerFactory builds a meross.New PowerFunc from a power config map:
// ip (required), key (optional but recommended), channel (optional).
func merossPowerFactory(config map[string]any) (sdwire.PowerFunc, error) {
	ip, _ := config["ip"].(string)
	if ip == "" {
		return nil, fmt.Errorf("meross power config requires \"ip\"")
	}
	key, _ := config["key"].(string)

	var opts []meross.Option
	if raw, ok := config["channel"]; ok {
		ch, err := toInt(raw)
		if err != nil {
			return nil, fmt.Errorf("meross power config: channel: %w", err)
		}
		opts = append(opts, meross.WithChannel(ch))
	}

	return meross.New(ip, key, opts...)
}

// toInt coerces a YAML/JSON-decoded value (int, float64, or numeric string)
// to an int.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("not an integer: %q", n)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("not an integer: %v", v)
	}
}
