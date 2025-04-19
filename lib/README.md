# Static RaptorQ Library

This directory contains the static version of the RaptorQ library (`librq_library.a`) for different platforms and architectures.

## Directory Structure

The library files are organized in platform-specific subdirectories following Go's standard naming convention:

```
lib/
├── README.md             # This file
├── darwin/               # macOS libraries
│   ├── amd64/            # Intel macOS
│   │   ├── librq_library.a
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