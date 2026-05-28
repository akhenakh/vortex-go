# Vortex Go

Go CGO bindings for the [Vortex](https://github.com/vortex-data/vortex) columnar file format. 

This package provides a high-performance, zero-copy (where possible) Go interface to the Vortex Rust C-API. It allows you to create schemas, build columnar arrays, and efficiently read/write `.vortex` files directly from Go.

## Prerequisites

Because this library binds to a Rust core, you will need the following installed on your system:
* **Go** (1.20+)
* **Rust** (cargo)
* **A C Compiler** (gcc/clang)

## Build Instructions

This project uses a Git submodule to lock the upstream Vortex repository and builds the static C FFI library automatically using `go generate`.

To clone and build the project, run:

```sh
# Initialize and pull the Rust source code submodule
git submodule update --init --recursive

# Compile the Rust vortex-ffi static library (libvortex_ffi.a)
go generate ./...

# Build the Go project
go build
```

## Quick Start Example

Here is a simple example demonstrating how to define a schema, write a batch of columnar data to a `.vortex` file, and then scan it back into memory.

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/akhenakh/vortex-go"
)

func main() {
	// 1. Initialize a new Vortex Session
	session := vortex.NewSession()
	defer session.Free() // Explicitly free C resources when done

	filePath := "sample.vortex"
	defer os.Remove(filePath) // Cleanup after example

  // Write to the file
  
	// 2. Define the Schema: Struct { id: u32, score: f64 }
	fb := vortex.NewStructFieldsBuilder()
	fb.AddField("id", vortex.NewPrimitiveDType(vortex.PTypeU32, false))
	fb.AddField("score", vortex.NewPrimitiveDType(vortex.PTypeF64, false))
	
	// Finalize fields and create the Struct DType
	schema := vortex.NewStructDType(fb.Finalize(), false)
	defer schema.Free()

	// 3. Create Data Arrays
	val := &vortex.Validity{Type: vortex.ValidityNonNullable}
	
	idArr, err := vortex.NewPrimitiveArray(vortex.PTypeU32, []uint32{1, 2, 3}, val)
	if err != nil {
		log.Fatal(err)
	}
	defer idArr.Free()

	scoreArr, err := vortex.NewPrimitiveArray(vortex.PTypeF64, []float64{95.5, 82.0, 42.1}, val)
	if err != nil {
		log.Fatal(err)
	}
	defer scoreArr.Free()

	// 4. Combine into a Struct Array (Batch)
	builder := vortex.NewStructColumnBuilder(val, 2)
	builder.AddField("id", idArr)
	builder.AddField("score", scoreArr)
	
	batch, err := builder.Finalize()
	if err != nil {
		log.Fatal(err)
	}
	defer batch.Free()

	// 5. Open a Sink and Write to Disk
	sink, err := vortex.OpenFileSink(session, filePath, schema)
	if err != nil {
		log.Fatal(err)
	}

	if err := sink.Push(batch); err != nil {
		log.Fatal(err)
	}
	
	if err := sink.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Successfully wrote 3 rows to %s\n", filePath)

  // Read from the file
  
	// 6. Open a Data Source
	ds, err := vortex.NewDataSource(session, vortex.DataSourceOptions{Paths: filePath})
	if err != nil {
		log.Fatal(err)
	}
	defer ds.Free()

	// 7. Perform a Scan (Reads all data. You can pass ScanOptions for projection/filtering)
	scan, est, err := ds.Scan(nil)
	if err != nil {
		log.Fatal(err)
	}
	defer scan.Free()

	fmt.Printf("Scan plan estimates %d partitions\n", est.Value)

	// 8. Consume the partition
	partition, err := scan.NextPartition()
	if err != nil {
		log.Fatal(err)
	}
	defer partition.Free()

	// 9. Extract the Array from the partition
	readArray, err := partition.Next()
	if err != nil {
		log.Fatal(err)
	}
	defer readArray.Free()

	fmt.Printf("Read array of length: %d\n", readArray.Len())

	// 10. Extract the "score" column (Index 1) and read values
	readScoreField, err := readArray.Field(1)
	if err != nil {
		log.Fatal(err)
	}
	defer readScoreField.Free()

	for i := 0; i < readScoreField.Len(); i++ {
		val := readScoreField.GetF64(i)
		fmt.Printf("Row %d Score: %.1f\n", i, val)
	}
}
```

## Memory Management

This package uses CGO to bridge Go and Rust. 

To ensure safety and prevent memory leaks, almost all Vortex objects (`Session`, `Array`, `DType`, `Scalar`, etc.) automatically register Go `runtime.SetFinalizer` hooks to free the underlying Rust memory when the Go objects are garbage collected. 

However, because Go's garbage collector does not know how much invisible C-memory is actually being held by a small pointer, **it is highly recommended to explicitly call `.Free()`** (usually via `defer`) on objects that allocate significant memory (like `Array` and `Session`), especially in tight loops or long-running applications. Doing so unregisters the finalizer and instantly frees the memory, resulting in much lower memory pressure.
