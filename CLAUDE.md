# extract-zone-file

CLI tool to extract DNS zone files from any domain.

## Module

`github.com/prenansantana/extract-zone-file`

## Commands

```bash
# Build
go build -o dzone .

# Run
./dzone <domain>

# Cross-compile all platforms
make build-all

# Clean build artifacts
make clean
```

## Project Structure

```
├── main.go              # Entry point, CLI argument parsing (flag stdlib)
├── resolver/
│   └── resolver.go      # DNS query logic (authoritative NS discovery, per-type queries, AXFR)
├── zone/
│   └── formatter.go     # BIND zone file output formatting
├── Makefile             # Cross-compilation targets
└── go.mod               # Single dependency: github.com/miekg/dns
```

## Conventions

- Go standard library for CLI parsing (`flag` package) — no external CLI frameworks
- Single external dependency: `github.com/miekg/dns`
- DNS queries run concurrently via goroutines with 5s timeout per query
- Output follows standard BIND zone file format
- `dns.RR.String()` from miekg/dns produces valid BIND record lines
- Internal package named `resolver` (not `dns`) to avoid shadowing `miekg/dns` import
