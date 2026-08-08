# power/meross

`sdwire.PowerFunc` for Meross smart plugs (MSS315 and compatible), driven over
the plug's **local HTTP API**. No cloud connection is needed at runtime —
only the plug's IP address and its account key, fetched once up front.

## Quick start

```go
package main

import (
    "log"

    "github.com/jphastings/sdwire"
    "github.com/jphastings/sdwire/power/meross"
)

func main() {
    powerFunc, err := meross.New("192.168.1.50", "your-account-key")
    if err != nil {
        log.Fatal(err)
    }

    sw, err := sdwire.New(sdwire.WithTargetPower(powerFunc))
    if err != nil {
        log.Fatal(err)
    }
    defer sw.Close()

    // Power-cycle the target board through the plug.
    if err := sw.PowerCycle(0); err != nil {
        log.Fatal(err)
    }
}
```

Need more than the relay switch? Use `meross.NewClient` directly for
`State`, `Model`, and `Electricity`:

```go
client, err := meross.NewClient("192.168.1.50", "your-account-key")
if err != nil {
    log.Fatal(err)
}

on, err := client.State()
model, mac, err := client.Model()
reading, err := client.Electricity() // MSS315 and other metering models only
```

## Getting your Meross account key

The key is per-**account**, not per-device — every plug registered to the
same Meross account shares it. A few ways to get it:

**Easiest: a short Python script**, using
[MerossIot](https://github.com/AlbertoGeniola/MerossIot):

```python
import asyncio
from meross_iot.http_api import MerossHttpClient

async def main():
    client = await MerossHttpClient.async_from_user_password(
        api_base_url="https://iot.meross.com",
        email="you@example.com",
        password="your-meross-password",
    )
    print(client.cloud_credentials.key)

asyncio.run(main())
```

**Home Assistant users**: the [`meross_lan`](https://github.com/krahabb/meross_lan)
integration's device diagnostics download includes the device key for each
plug it manages — no extra script needed if you already run it.

**Very old firmware**: some early Meross plugs accept an empty key. It's
worth passing `""` first — `meross.NewClient` and `meross.New` both allow
it — before going to the trouble of fetching real credentials.

## Finding the plug's IP

There's no discovery in this package yet: give it the plug's IP address
directly, and **DHCP-reserve that address** (a static IP assigned by your
router based on the plug's MAC address) so it doesn't change under you.
The `Model` method returns the plug's MAC address if you need it to set up
the reservation. mDNS / Meross-UDP based discovery is a plausible future
addition, but isn't implemented here.

## Caveats

- **Metering lag.** `Electricity` readings lag the true instantaneous
  values by several seconds inside the plug's firmware. Trust the trend
  across repeated calls, not any single reading.
- **Relay-off is not board-off.** The plug's own control electronics stay
  powered even with the relay open, so it stays reachable after
  `SetPower(false)`. Small target boards can also ride through a brief
  interruption on PSU bulk capacitance without actually resetting. Callers
  that need a guaranteed power cycle (e.g. `sdwire.SDWire.PowerCycle`) are
  responsible for holding power off for a sufficient minimum dark time —
  this package only guarantees the relay itself has switched.
