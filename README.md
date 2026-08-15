# vectorcore-LCS

VectorCore LCS is a web console for requesting UE (subscriber) locations from a GMLC
over the standard **Le interface — OMA MLP (Mobile Location Protocol) v3.5 over
XML/HTTP**. It's a single Go binary that serves a small proxy API plus an embedded
React UI; the browser never talks to the GMLC directly.

## Features

- **Locate a UE by IMSI or MSISDN** — submits a Standard Location Immediate
  (slir/slia) request and polls until it reaches a terminal state.
- **Optional QoS controls** — accuracy class, horizontal/vertical accuracy,
  vertical-requested, and response-time preference, each independently optional.
- **Live result map** — plots the returned position (circle or ellipse
  uncertainty) using Leaflet.
- **Location history** — queries the GMLC's Historic Location Immediate service
  for a target over a selectable time window (1h / 24h / 7d / 30d) and lets you
  preview any past fix on the map.
- **GMLC status indicator** — polls reachability/readiness in the background so
  the console always shows whether the GMLC is reachable and ready.
- **Emergency console** — a separate `/emergency` route bound to its own
  independently configured GMLC client credential, so emergency requests are
  authenticated (and thus classified as `LCS-Client-Type: Emergency` on the GMLC
  side) distinctly from ordinary lookups. The nav entry only appears when an
  `emergency` profile is actually configured.
- **Multiple GMLC client profiles** — any number of named profiles
  (`gmlc_clients` in `config/lcs.yaml`), each with its own base URL and
  credentials, all speaking MLP.

## Build requirements

- **Go** 1.26.2 or later (see `go.mod`)
- **Node.js / npm** (for building the React UI — see `web/package.json`)
- `make`
- Linux with `systemd` (only needed for `make install` / `make uninstall`)

## Building

```bash
make clean   # remove bin/
make         # build the UI, tidy Go deps, and compile bin/lcs
```

`make` (the default `build` target) runs, in order:

1. `ui` — `cd web && npm install && npm run build`, producing `web/dist`, which
   is embedded into the binary (see `web/embed.go`).
2. `deps` — `go mod tidy`.
3. `go build -o bin/lcs ./cmd/lcs`.

Other targets:

| Target | Purpose |
|---|---|
| `make ui` | Build only the React UI. |
| `make dev-ui` | Start the Vite dev server (proxies `/api` to `localhost:8090`). |
| `make deps` | `go mod tidy`. |
| `make install` | Build, then install the binary, config, and systemd unit under `/opt/vectorcore` and enable/start the `vectorcore-lcs` service. |
| `make uninstall` | Stop/disable the service and remove the installed binary and unit file. |
| `make clean` | Remove `bin/`. |

## Running

```bash
./bin/lcs -c config/lcs.yaml
```

Flags:

| Flag | Purpose |
|---|---|
| `-c` | Path to the YAML config file (default `config/lcs.yaml`). |
| `-v` / `-version` | Print the version and exit. |
| `-d` | Also log to stdout regardless of the config's log settings. |

See `config/lcs.yaml` for the configuration reference (server address, one or
more `gmlc_clients` profiles, polling, and logging).
