// Package rq_go provides Go bindings for the RaptorQ erasure coding library.
// RaptorQ is a Forward Error Correction (FEC) code that allows for efficient recovery
// of data from partial information. This package enables encoding files into RaptorQ
// symbols and decoding them back, providing resilience against data loss.
package rq_go

/*
#cgo CFLAGS: -I${SRCDIR}/include

// Platform-specific LDFLAGS for static linking
#cgo linux LDFLAGS: -L${SRCDIR}/lib/linux/amd64 -Wl,-Bstatic -lrq_library -Wl,-Bdynamic -ldl -lm -lpthread
#cgo darwin LDFLAGS: -L${SRCDIR}/lib/darwin/amd64 -lrq_library -framework Security -framework CoreFoundation -lm
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/lib/darwin/arm64 -lrq_library -framework Security -framework CoreFoundation -lm
#cgo windows LDFLAGS: -L${SRCDIR}/lib/windows/amd64 -lrq_library -lws2_32 -luserenv -ladvapi32

#include <stdlib.h>
#include <stdint.h>
#include <stdbool.h>
#include <rq-library.h>

// Function declarations matching the Rust library's exported functions
extern uintptr_t raptorq_init_session(uint16_t symbol_size, uint8_t redundancy_factor, uint64_t max_memory_mb, uint64_t concurrency_limit);
extern _Bool raptorq_free_session(uintptr_t session_id);
extern int32_t raptorq_encode_file(uintptr_t session_id, const char *input_path, const char *output_dir, uintptr_t block_size, char *result_buffer, uintptr_t result_buffer_len);
extern int32_t raptorq_create_metadata(uintptr_t session_id, const char *input_path, const char *layout_file, uintptr_t block_size, char *result_buffer, uintptr_t result_buffer_len);
extern int32_t raptorq_get_last_error(uintptr_t session_id, char *error_buffer, uintptr_t error_buffer_len);
extern int32_t raptorq_decode_symbols(uintptr_t session_id, const char *symbols_dir, const char *output_path, const char *layout_path);
extern uintptr_t raptorq_get_recommended_block_size(uintptr_t session_id, uint64_t file_size);
extern int32_t raptorq_version(char *version_buffer, uintptr_t version_buffer_len);
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Default configuration values for the RaptorQ processor.
// These values match the defaults in the Rust library src/lib.rs and provide
// a good balance between performance and resource usage for most use cases.
const (
	// DefaultSymbolSize is the default size of each symbol in bytes.
	// The symbol size affects encoding/decoding performance and memory usage.
	DefaultSymbolSize uint16 = 65535 // 64KB - 1 byte

	// DefaultRedundancyFactor determines how many repair symbols are generated.
	// Higher values provide better recovery capability but increase storage requirements.
	// A value of 4 means 4 repair symbols will be generated for each source symbol.
	DefaultRedundancyFactor uint8 = 4

	// DefaultMaxMemoryMB limits the maximum memory usage during encoding/decoding.
	// Default is 16GB, which is suitable for most modern systems.
	DefaultMaxMemoryMB uint64 = 16 * 1024

	// DefaultConcurrencyLimit controls the maximum number of concurrent operations.
	// This helps manage CPU usage and prevents system overload.
	DefaultConcurrencyLimit uint64 = 4

	// MaxMemoryMB_4GB provides a 4GB memory limit option for systems with less RAM.
	// Useful for embedded systems or when running in constrained environments.
	MaxMemoryMB_4GB uint64 = 4 * 1024

	// The following memory limit options are currently disabled but can be uncommented if needed:
	// MaxMemoryMB_1GB uint64 = 1 * 1024
	// MaxMemoryMB_8GB uint64 = 8 * 1024
)

// sessionMutex protects concurrent access to the sessions map.
// This ensures thread-safety when creating or freeing RaptorQ sessions.
var sessionMutex sync.Mutex

// sessions tracks all active RaptorQ processing sessions.
// The map uses session IDs as keys and empty structs as values to minimize memory usage.
var sessions = make(map[uintptr]struct{})

// RaptorQProcessor represents a RaptorQ processing session that handles encoding and decoding operations.
// Each processor maintains its own state and configuration, allowing for concurrent processing
// of multiple files with different settings. The processor must be freed when no longer needed
// to prevent memory leaks.
type RaptorQProcessor struct {
	// SessionID is the unique identifier for this processing session in the underlying C library.
	// It's used in all C function calls to identify the specific session.
	SessionID uintptr
}

// ProcessorConfig holds configuration parameters for the RaptorQ processor.
// These settings control the behavior of the encoding and decoding processes,
// affecting performance, resource usage, and recovery capabilities.
type ProcessorConfig struct {
	// SymbolSize defines the size of each symbol in bytes.
	// Larger symbols can improve throughput but increase memory usage.
	SymbolSize uint16 `json:"symbol_size"`

	// RedundancyFactor determines how many repair symbols are generated per source symbol.
	// Higher values provide better recovery capability but increase storage requirements.
	RedundancyFactor uint8 `json:"redundancy_factor"`

	// MaxMemoryMB limits the maximum memory usage during encoding/decoding in megabytes.
	// This prevents the process from consuming too much system memory.
	MaxMemoryMB uint64 `json:"max_memory_mb"`

	// ConcurrencyLimit controls the maximum number of concurrent operations.
	// This helps manage CPU usage and prevents system overload.
	ConcurrencyLimit uint64 `json:"concurrency_limit"`
}

// ProcessResult holds information about the results of an encoding or metadata creation operation.
// It contains details about the generated symbols and the layout of the encoded data,
// which are necessary for the decoding process.
type ProcessResult struct {
	// TotalSymbolsCount is the total number of symbols generated (source + repair).
	TotalSymbolsCount uint32 `json:"total_symbols_count"`

	// TotalRepairSymbols is the number of repair symbols generated.
	TotalRepairSymbols uint32 `json:"total_repair_symbols"`

	// SymbolsDirectory is the path to the directory containing the generated symbols.
	SymbolsDirectory string `json:"symbols_directory"`

	// Blocks contains information about each encoded block.
	// This field may be omitted in some responses.
	Blocks []Block `json:"blocks,omitempty"`

	// LayoutFilePath is the path to the file containing the layout information.
	// This file is required for decoding and contains metadata about the encoding process.
	LayoutFilePath string `json:"layout_file_path"`
}

// Block represents information about a processed data block during encoding.
// Files are split into blocks for processing, and each block has its own set of
// source and repair symbols. This structure contains the metadata needed to
// identify and decode a specific block.
type Block struct {
	// BlockID is the unique identifier for this block.
	BlockID uint64 `json:"block_id"`

	// EncoderParameters contains parameters used by the RaptorQ algorithm for this block.
	// These are required for the decoding process.
	EncoderParameters []uint8 `json:"encoder_parameters"`

	// OriginalOffset is the offset of this block in the original file.
	OriginalOffset uint64 `json:"original_offset"`

	// Size is the size of this block in bytes.
	Size uint64 `json:"size"`

	// SymbolsCount is the total number of symbols in this block (source + repair).
	SymbolsCount uint32 `json:"symbols_count"`

	// SourceSymbolsCount is the number of source symbols in this block.
	// Source symbols contain the original data, while repair symbols provide redundancy.
	SourceSymbolsCount uint32 `json:"source_symbols_count"`

	// Hash is a hash of the block's data, used for integrity verification.
	Hash string `json:"hash"`
}

// NewRaptorQProcessor creates a new RaptorQ processor with the specified configuration.
//
// This function initializes a new session with the underlying RaptorQ library and
// returns a processor that can be used for encoding and decoding operations.
//
// Parameters:
//   - symbolSize: Size of each symbol in bytes. Larger symbols can improve throughput
//     but increase memory usage. Maximum value is 65535 (64KB - 1 byte).
//   - redundancyFactor: Determines how many repair symbols are generated per source symbol.
//     Higher values provide better recovery capability but increase storage requirements.
//   - maxMemoryMB: Maximum memory usage limit in megabytes. This prevents the process
//     from consuming too much system memory during encoding/decoding operations.
//   - concurrencyLimit: Maximum number of concurrent operations. This helps manage
//     CPU usage and prevents system overload.
//
// Returns:
//   - *RaptorQProcessor: A new processor instance if successful.
//   - error: An error if the session initialization fails.
//
// The returned processor must be freed when no longer needed by calling the Free() method
// to prevent memory leaks, although a finalizer is set to handle cleanup if Free() is not called.
func NewRaptorQProcessor(symbolSize uint16, redundancyFactor uint8, maxMemoryMB uint64, concurrencyLimit uint64) (*RaptorQProcessor, error) {
	sessionID := C.raptorq_init_session(
		C.uint16_t(symbolSize),
		C.uint8_t(redundancyFactor),
		C.uint64_t(maxMemoryMB),
		C.uint64_t(concurrencyLimit),
	)

	if sessionID == 0 {
		return nil, fmt.Errorf("failed to initialize RaptorQ session")
	}

	// Register session
	sessionMutex.Lock()
	sessions[uintptr(sessionID)] = struct{}{}
	sessionMutex.Unlock()

	processor := &RaptorQProcessor{
		SessionID: uintptr(sessionID),
	}

	// Set finalizer to clean up session
	runtime.SetFinalizer(processor, finalizeProcessor)

	return processor, nil
}

// NewDefaultRaptorQProcessor creates a new RaptorQ processor with default configuration.
//
// This is a convenience function that creates a processor with the following default values:
//   - Symbol size: 65535 bytes (64KB - 1 byte)
//   - Redundancy factor: 4 (4 repair symbols per source symbol)
//   - Max memory: 16GB
//   - Concurrency limit: 4 threads
//
// Returns:
//   - *RaptorQProcessor: A new processor instance with default configuration if successful.
//   - error: An error if the session initialization fails.
//
// Example:
//
//	processor, err := raptorq.NewDefaultRaptorQProcessor()
//	if err != nil {
//	    log.Fatalf("Failed to create processor: %v", err)
//	}
//	defer processor.Free()
func NewDefaultRaptorQProcessor() (*RaptorQProcessor, error) {
	return NewRaptorQProcessor(
		DefaultSymbolSize,
		DefaultRedundancyFactor,
		DefaultMaxMemoryMB,
		DefaultConcurrencyLimit,
	)
}

// Free manually frees the RaptorQ session and releases associated resources.
//
// This method should be called when the processor is no longer needed to ensure
// that all resources are properly released. It's recommended to use defer immediately
// after creating a processor to ensure cleanup happens even if errors occur:
//
//	processor, err := raptorq.NewDefaultRaptorQProcessor()
//	if err != nil {
//	    return err
//	}
//	defer processor.Free()
//
// Returns:
//   - true: If the session was successfully freed.
//   - false: If the session was already freed or if freeing failed.
//
// Note that a finalizer is set to call this method automatically during garbage collection
// if it's not called manually, but explicit calls are preferred for deterministic cleanup.
func (p *RaptorQProcessor) Free() bool {
	if p.SessionID != 0 {
		result := C.raptorq_free_session(C.uintptr_t(p.SessionID))
		success := bool(result)

		if success {
			// Unregister session
			sessionMutex.Lock()
			delete(sessions, p.SessionID)
			sessionMutex.Unlock()

			p.SessionID = 0
		}

		return success
	}
	return false
}

// finalizeProcessor is a finalizer function for RaptorQProcessor instances.
//
// This function is automatically called by the Go runtime during garbage collection
// for any RaptorQProcessor instances that haven't been manually freed. It ensures
// that resources are released even if the user forgets to call Free().
//
// While this provides a safety net, it's still recommended to explicitly call Free()
// when done with a processor for more deterministic resource management.
func finalizeProcessor(p *RaptorQProcessor) {
	p.Free()
}

// EncodeFile encodes a file using the RaptorQ erasure coding algorithm.
//
// This function reads the file at inputPath, encodes it using RaptorQ, and writes
// the resulting symbols to the outputDir directory. The file is split into blocks
// of the specified size, and each block is encoded separately.
//
// Parameters:
//   - inputPath: Path to the input file to be encoded.
//   - outputDir: Directory where the encoded symbols will be written.
//   - blockSize: Size of each block in bytes. If 0, a recommended block size will be used.
//     Larger blocks can improve efficiency but require more memory during processing.
//
// Returns:
//   - *ProcessResult: Information about the encoding process, including the total number
//     of symbols generated, the path to the symbols directory, and metadata about each block.
//   - error: An error if the encoding process fails. The error message will include details
//     about the specific failure.
//
// Possible error conditions include:
//   - Session closed
//   - File not found
//   - I/O errors
//   - Memory limit exceeded
//   - Concurrency limit reached
//   - Invalid parameters
//
// Example:
//
//	processor, err := raptorq.NewDefaultRaptorQProcessor()
//	if err != nil {
//	    return err
//	}
//	defer processor.Free()
//
//	// Use 1MB blocks for encoding
//	result, err := processor.EncodeFile("input.dat", "symbols/", 1024*1024)
//	if err != nil {
//	    return fmt.Errorf("encoding failed: %w", err)
//	}
//
//	fmt.Printf("Encoded file with %d total symbols\n", result.TotalSymbolsCount)
func (p *RaptorQProcessor) EncodeFile(inputPath, outputDir string, blockSize int) (*ProcessResult, error) {
	if p.SessionID == 0 {
		return nil, fmt.Errorf("RaptorQ session is closed")
	}

	cInputPath := C.CString(inputPath)
	defer C.free(unsafe.Pointer(cInputPath))

	cOutputDir := C.CString(outputDir)
	defer C.free(unsafe.Pointer(cOutputDir))

	// Buffer for result (16KB should be enough for metadata)
	resultBufSize := 16 * 1024
	resultBuf := (*C.char)(C.malloc(C.size_t(resultBufSize)))
	defer C.free(unsafe.Pointer(resultBuf))

	res := C.raptorq_encode_file(
		C.uintptr_t(p.SessionID),
		cInputPath,
		cOutputDir,
		C.uintptr_t(blockSize),
		resultBuf,
		C.uintptr_t(resultBufSize),
	)

	switch res {
	case 0:
		// Success
	case -1:
		return nil, fmt.Errorf("generic error")
	case -2:
		return nil, fmt.Errorf("invalid parameters")
	case -3:
		return nil, fmt.Errorf("invalid response (JSON serialization error)")
	case -4:
		return nil, fmt.Errorf("result buffer too small")
	case -5:
		return nil, fmt.Errorf("invalid session")
	case -11:
		return nil, fmt.Errorf("IO error: %s", p.getLastError())
	case -12:
		return nil, fmt.Errorf("file not found: %s", p.getLastError())
	case -13:
		return nil, fmt.Errorf("encoding failed: %s", p.getLastError())
	case -15:
		return nil, fmt.Errorf("memory limit exceeded: %s", p.getLastError())
	case -16:
		return nil, fmt.Errorf("concurrency limit reached: %s", p.getLastError())
	default:
		return nil, fmt.Errorf("unknown error code %d: %s", res, p.getLastError())
	}

	// Parse the JSON result
	resultJSON := C.GoString(resultBuf)
	var result ProcessResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// CreateMetadata creates metadata for RaptorQ encoding without writing symbol data.
//
// This function analyzes the file at inputPath and generates the metadata needed for
// RaptorQ encoding, but doesn't actually create the encoded symbols. This is useful for
// planning purposes or for two-phase encoding processes where metadata generation and
// symbol creation are separate steps.
//
// The function writes the layout information to the specified layout file, which contains
// details about how the file would be split into blocks and encoded.
//
// Parameters:
//   - inputPath: Path to the input file to analyze.
//   - layoutFile: Path where the layout information will be written.
//   - blockSize: Size of each block in bytes. If 0, a recommended block size will be used.
//     Larger blocks can improve efficiency but require more memory during processing.
//
// Returns:
//   - *ProcessResult: Information about the metadata creation process, including details
//     about how the file would be encoded (block count, symbol count, etc.).
//   - error: An error if the metadata creation process fails. The error message will include
//     details about the specific failure.
//
// Possible error conditions include:
//   - Session closed
//   - File not found
//   - I/O errors
//   - Invalid paths
//   - Memory limit exceeded
//   - Concurrency limit reached
//   - Invalid parameters
//
// Example:
//
//	processor, err := raptorq.NewDefaultRaptorQProcessor()
//	if err != nil {
//	    return err
//	}
//	defer processor.Free()
//
//	// Generate metadata using 1MB blocks
//	result, err := processor.CreateMetadata("input.dat", "layout.json", 1024*1024)
//	if err != nil {
//	    return fmt.Errorf("metadata creation failed: %w", err)
//	}
//
//	fmt.Printf("File would be encoded with %d total symbols\n", result.TotalSymbolsCount)
func (p *RaptorQProcessor) CreateMetadata(inputPath, layoutFile string, blockSize int) (*ProcessResult, error) {
	if p.SessionID == 0 {
		return nil, fmt.Errorf("RaptorQ session is closed")
	}

	cInputPath := C.CString(inputPath)
	defer C.free(unsafe.Pointer(cInputPath))

	cLayoutFile := C.CString(layoutFile)
	defer C.free(unsafe.Pointer(cLayoutFile))

	// Buffer for result (16KB should be enough for metadata)
	resultBufSize := 16 * 1024
	resultBuf := (*C.char)(C.malloc(C.size_t(resultBufSize)))
	defer C.free(unsafe.Pointer(resultBuf))

	res := C.raptorq_create_metadata(
		C.uintptr_t(p.SessionID),
		cInputPath,
		cLayoutFile,
		C.uintptr_t(blockSize),
		resultBuf,
		C.uintptr_t(resultBufSize),
	)

	switch res {
	case 0:
		// Success
	case -1:
		return nil, fmt.Errorf("generic error")
	case -2:
		return nil, fmt.Errorf("invalid parameters")
	case -3:
		return nil, fmt.Errorf("invalid response (JSON serialization error)")
	case -4:
		return nil, fmt.Errorf("result buffer too small")
	case -5:
		return nil, fmt.Errorf("invalid session")
	case -11:
		return nil, fmt.Errorf("IO error: %s", p.getLastError())
	case -12:
		return nil, fmt.Errorf("file not found: %s", p.getLastError())
	case -13:
		return nil, fmt.Errorf("invalid path: %s", p.getLastError())
	case -14:
		return nil, fmt.Errorf("encoding failed: %s", p.getLastError())
	case -16:
		return nil, fmt.Errorf("memory limit exceeded: %s", p.getLastError())
	case -17:
		return nil, fmt.Errorf("concurrency limit reached: %s", p.getLastError())
	default:
		return nil, fmt.Errorf("unknown error code %d: %s", res, p.getLastError())
	}

	// Parse the JSON result
	resultJSON := C.GoString(resultBuf)
	var result ProcessResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

// DecodeSymbols decodes RaptorQ symbols back to the original file.
//
// This function reads encoded symbols from the symbolsDir directory and uses the
// layout information from layoutPath to reconstruct the original file at outputPath.
// The RaptorQ algorithm can recover the original file even if some symbols are missing,
// as long as enough symbols are available.
//
// Parameters:
//   - symbolsDir: Directory containing the encoded symbols.
//   - outputPath: Path where the reconstructed file will be written.
//   - layoutPath: Path to the layout file containing metadata about the encoding.
//     This file is created during the encoding process or by CreateMetadata.
//
// Returns:
//   - error: An error if the decoding process fails. The error message will include
//     details about the specific failure. Returns nil on success.
//
// Possible error conditions include:
//   - Session closed
//   - Empty parameters
//   - File not found
//   - I/O errors
//   - Insufficient symbols for recovery
//   - Memory limit exceeded
//   - Concurrency limit reached
//   - Invalid parameters
//
// Example:
//
//	processor, err := raptorq.NewDefaultRaptorQProcessor()
//	if err != nil {
//	    return err
//	}
//	defer processor.Free()
//
//	// Decode symbols back to the original file
//	err = processor.DecodeSymbols("symbols/", "recovered.dat", "layout.json")
//	if err != nil {
//	    return fmt.Errorf("decoding failed: %w", err)
//	}
//
//	fmt.Println("File successfully recovered")
func (p *RaptorQProcessor) DecodeSymbols(symbolsDir, outputPath, layoutPath string) error {
	if p.SessionID == 0 {
		return fmt.Errorf("RaptorQ session is closed")
	}

	// Input validation
	if symbolsDir == "" || outputPath == "" || layoutPath == "" {
		return fmt.Errorf("symbolsDir, outputPath, and layoutPath cannot be empty")
	}

	cSymbolsDir := C.CString(symbolsDir)
	defer C.free(unsafe.Pointer(cSymbolsDir))

	cOutputPath := C.CString(outputPath)
	defer C.free(unsafe.Pointer(cOutputPath))

	cLayoutPath := C.CString(layoutPath)
	defer C.free(unsafe.Pointer(cLayoutPath))

	res := C.raptorq_decode_symbols(
		C.uintptr_t(p.SessionID),
		cSymbolsDir,
		cOutputPath,
		cLayoutPath,
	)

	switch res {
	case 0:
		return nil
	case -1:
		return fmt.Errorf("generic error")
	case -2:
		return fmt.Errorf("invalid parameters")
	case -3:
		return fmt.Errorf("invalid response") // Assuming similar meaning as encode
	case -4:
		return fmt.Errorf("bad return buffer size") // Assuming similar meaning as encode
	case -5:
		return fmt.Errorf("invalid session")
	case -11:
		return fmt.Errorf("IO error: %s", p.getLastError())
	case -12:
		return fmt.Errorf("file not found: %s", p.getLastError())
	case -14:
		return fmt.Errorf("decoding failed: %s", p.getLastError())
	case -15:
		return fmt.Errorf("memory limit exceeded: %s", p.getLastError())
	case -16:
		return fmt.Errorf("concurrency limit reached: %s", p.getLastError())
	default:
		return fmt.Errorf("unknown error code %d: %s", res, p.getLastError())
	}
}

// GetRecommendedBlockSize returns a recommended block size for a file of the given size.
//
// This function calculates an optimal block size based on the file size and the
// processor's configuration. Using the recommended block size can improve encoding
// and decoding performance and efficiency.
//
// The recommended block size balances several factors:
//   - Memory usage during encoding/decoding
//   - Processing speed
//   - Recovery capabilities
//   - Symbol count and management
//
// Parameters:
//   - fileSize: Size of the file in bytes.
//
// Returns:
//   - int: The recommended block size in bytes. Returns 0 if the session is closed.
//
// Example:
//
//	processor, err := raptorq.NewDefaultRaptorQProcessor()
//	if err != nil {
//	    return err
//	}
//	defer processor.Free()
//
//	fileInfo, err := os.Stat("large_file.dat")
//	if err != nil {
//	    return err
//	}
//
//	blockSize := processor.GetRecommendedBlockSize(uint64(fileInfo.Size()))
//	fmt.Printf("Recommended block size: %d bytes\n", blockSize)
func (p *RaptorQProcessor) GetRecommendedBlockSize(fileSize uint64) int {
	if p.SessionID == 0 {
		return 0
	}

	return int(C.raptorq_get_recommended_block_size(
		C.uintptr_t(p.SessionID),
		C.uint64_t(fileSize),
	))
}

// GetVersion returns the version of the underlying RaptorQ library.
//
// This function queries the C library for its version string, which typically
// includes the version number and possibly build information. This can be useful
// for logging, debugging, or ensuring compatibility with specific library versions.
//
// Returns:
//   - string: The version string of the RaptorQ library. Returns "Unknown version"
//     if the version information cannot be retrieved.
//
// Example:
//
//	version := raptorq.GetVersion()
//	fmt.Printf("Using RaptorQ library version: %s\n", version)
func GetVersion() string {
	bufSize := 128
	versionBuf := (*C.char)(C.malloc(C.size_t(bufSize)))
	defer C.free(unsafe.Pointer(versionBuf))

	res := C.raptorq_version(versionBuf, C.size_t(bufSize))

	if res != 0 {
		return "Unknown version"
	}

	return C.GoString(versionBuf)
}

// getLastError retrieves the last error message from the RaptorQ library for this session.
//
// This is an internal function used to get detailed error information when a library
// function returns an error code. The error message provides more context about what
// went wrong during the operation.
//
// Returns:
//   - string: The last error message from the library. Returns "Session closed" if
//     the session is no longer valid, or "Error retrieving error message" if the
//     error information cannot be retrieved.
func (p *RaptorQProcessor) getLastError() string {
	if p.SessionID == 0 {
		return "Session closed"
	}

	bufSize := 1024
	errorBuf := (*C.char)(C.malloc(C.size_t(bufSize)))
	defer C.free(unsafe.Pointer(errorBuf))

	result := C.raptorq_get_last_error(
		C.uintptr_t(p.SessionID),
		errorBuf,
		C.size_t(bufSize),
	)

	if result != 0 {
		return "Error retrieving error message"
	}

	return C.GoString(errorBuf)
}
