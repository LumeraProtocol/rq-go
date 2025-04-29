# RaptorQ Go Bindings

This repository contains Go bindings for the RaptorQ erasure coding library. It is automatically synchronized from the [rq-library](https://github.com/LumeraProtocol/rq-library) repository.

## Repository Structure

```
rq-go/
├── README.md                 # This file
├── go.mod                    # Root Go module file
├── raptorq.go                # Main Go binding code
└── raptorq_test.go           # Tests for the Go bindings
└── lib/                      # Platform-specific libraries
    ├── README.md             # Documentation for the libraries
    ├── darwin/               # macOS libraries
    │   ├── amd64/            # Intel macOS
    │   │   └── librq_library.a
    │   └── arm64/            # Apple Silicon
    │       └── librq_library.a
    ├── linux/                # Linux libraries
    │   ├── amd64/            # amd64
    │   │   └── librq_library.a
    │   └── arm64/            # ARM64
    │       └── librq_library.a
    └── windows/              # Windows libraries
        └── amd64/            # amd64
            └── librq_library.a
```

## Platform Subfolders

The platform subfolder structure follows Go's standard naming convention:
- `darwin/amd64` - macOS on Intel
- `darwin/arm64` - macOS on Apple Silicon
- `linux/amd64` - Linux on amd64
- `linux/arm64` - Linux on ARM64 (including Raspberry Pi)
- `windows/amd64` - Windows on amd64

## Linking

The Go module is configured to statically link with the RaptorQ library. This means the library code is included in the final binary, making it self-contained and avoiding runtime dependencies on shared libraries.

## Usage

To use this package in your Go project:

```bash
go get github.com/LumeraProtocol/rq-go
```

Then import it in your code:

```go
import "github.com/LumeraProtocol/rq-go"
```

## Example

```go
package main

import (
	"fmt"
	"github.com/LumeraProtocol/rq-go"
)

func main() {
	// Create a new RaptorQ processor with default settings
	processor, err := raptorq.NewDefaultRaptorQProcessor()
	if err != nil {
		panic(err)
	}
	defer processor.Free()

	// Get library version
	version := raptorq.GetVersion()
	fmt.Printf("RaptorQ library version: %s\n", version)

	// Use the processor to encode/decode files
	// See the test file for examples
}
```

## License

This project is licensed under MIT License. See the [LICENSE](LICENSE) file for details.
