package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/prenansantana/extract-zone-file/resolver"
	"github.com/prenansantana/extract-zone-file/zone"
)

var version = "dev"

func main() {
	server := flag.String("s", "", "DNS server to query (default: auto-detect authoritative)")
	output := flag.String("o", "", "Output file (default: stdout)")
	types := flag.String("t", "", "Record types to query, comma-separated (default: all)")
	tryAXFR := flag.Bool("try-axfr", true, "Attempt zone transfer first")
	ver := flag.Bool("v", false, "Show version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: dzone <domain> [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *ver {
		fmt.Println("dzone", version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) != 1 {
		flag.Usage()
		os.Exit(1)
	}

	domain := args[0]

	rs, err := resolver.Resolve(domain, *server, *tryAXFR, *types)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	zoneFile := zone.Format(rs)

	if *output != "" {
		err := os.WriteFile(*output, []byte(zoneFile), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Zone file written to %s\n", *output)
	} else {
		fmt.Print(zoneFile)
	}
}
