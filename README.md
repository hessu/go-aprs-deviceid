# go-aprs-deviceid

APRS device identifier - Go edition

This is a Go port of the
[Ham::APRS::DeviceID](https://github.com/hessu/perl-aprs-deviceid) Perl
module. It identifies the manufacturer and model of an
APRS transmitter by examining a parsed APRS packet (as provided by
[go-aprs-fap](https://github.com/hessu/go-aprs-fap)) and looking up the
device in the [tocalls.yaml](https://github.com/aprsorg/aprs-deviceid)
identification database.

## AI-assisted port

This Go module was created using the Claude Code AI agent, by letting it
inspect the old Perl code. The original Perl module's identification logic
has been retained in the Go implementation.

## Identification methods

- **Tocall matching**: exact and wildcard destination callsign patterns
- **Mic-E legacy devices**: Kenwood TH-D7A, TH-D72, TH-D74, TM-D700,
  TM-D710 (identified by comment prefix/suffix)
- **Mic-E new-style suffix codes**: Yaesu, Byonics, Anytone, and other
  devices using 2-character Mic-E device suffixes
- **Messaging capability**: detected from Mic-E prefix character or
  device feature flags

## Installation

```
go get github.com/hessu/go-aprs-deviceid
```

You will also need a copy of `tocalls.yaml` from the
[aprs-deviceid](https://github.com/aprsorg/aprs-deviceid) repository.

## Usage

```go
package main

import (
    "fmt"
    "github.com/hessu/go-aprs-fap"
    "github.com/hessu/go-aprs-deviceid"
)

func main() {
    // Open the device identification database
    db, err := deviceid.Open("tocalls.yaml")
    if err != nil {
        fmt.Printf("Error loading DB: %v\n", err)
        return
    }

    // Parse a packet
    raw := "N0CALL>APDR15,WIDE1-1:!6028.51N/02505.68E#PHG2360"
    p, err := fap.Parse(raw)
    if err != nil {
        fmt.Printf("Parse error: %v\n", err)
        return
    }

    // Identify the device
    did, comment, err := db.Identify(p)
    if err != nil {
        fmt.Printf("Unknown device: %v\n", err)
        return
    }

    fmt.Printf("Vendor: %s\n", did.Vendor)
    fmt.Printf("Model: %s\n", did.Model)
    fmt.Printf("Comment: %s\n", comment)
}
```

## Hot-reloading

The `DeviceDB` is concurrency-safe. You can call `Identify` from multiple
goroutines simultaneously. To pick up changes to the `tocalls.yaml` file
without restarting, call `CheckReload` periodically from a single goroutine:

```go
reloaded, err := db.CheckReload()
if err != nil {
    log.Printf("Reload error: %v", err)
} else if reloaded {
    log.Println("Database reloaded")
}
```

`CheckReload` compares the file's inode and mtime and only reparses when
the file has actually changed. You can optionally pass a new path to switch
to a different YAML file:

```go
reloaded, err := db.CheckReload("/new/path/tocalls.yaml")
```

## Device identification database

The `tocalls.yaml` database is maintained separately at
<https://github.com/aprsorg/aprs-deviceid> and is available in YAML, JSON
and XML formats for use in other applications and programming languages.
It is licensed under the CC BY-SA 2.0 license.

## See also

- [Ham::APRS::DeviceID](https://github.com/hessu/perl-aprs-deviceid) - the original Perl module
- [aprs-deviceid](https://github.com/aprsorg/aprs-deviceid) - the tocalls.yaml device database
- [go-aprs-fap](https://github.com/hessu/go-aprs-fap) - Go APRS packet parser

## Copyright and licence

Copyright (C) 2026 Heikki Hannikainen

Available under the 3-clause BSD license, details in LICENSE.
