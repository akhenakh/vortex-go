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


## Examples


Using NYC Yellow Taxi Trip dataset:
```sh
cd cmd/yellowtrip
curl -O https://d37ci6vzurychx.cloudfront.net/trip-data/yellow_tripdata_2024-01.parquet
vx convert yellow_tripdata_2024-01.parquet 
go run .
```


Here is an optimized (7s to 35ms) version using arrow to aggregate:

```go
package main

import (
	"fmt"
	"io"
	"log"
	"sort"
	"time"
	"unsafe"

	"github.com/akhenakh/vortex-go" 

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/cdata"
)

// LocationStats holds our aggregated data
type LocationStats struct {
	PULocationID  int64
	NumTrips      uint64
	TotalDistance float64
	TotalAmount   float64
}

func main() {
	start := time.Now()

	// Initialize the Vortex Session
	session := vortex.NewSession()
	defer session.Free()

	filePath := "yellow_tripdata_2024-01.vortex"
	fmt.Printf("Analyzing %s...\n", filePath)

	// Open the Data Source
	ds, err := vortex.NewDataSource(session, vortex.DataSourceOptions{Paths: filePath})
	if err != nil {
		log.Fatalf("Failed to open data source: %v", err)
	}
	defer ds.Free()

	// Define Projection Options
	root := vortex.NewRootExpression()
	defer root.Free()

	// Vortex needs exactly the names as they appear in the file.
	projection := root.Select([]string{"PULocationID", "trip_distance", "total_amount"})
	defer projection.Free()

	opts := &vortex.ScanOptions{
		Projection: projection,
	}

	// Start the Scan
	scan, est, err := ds.Scan(opts)
	if err != nil {
		log.Fatalf("Failed to scan: %v", err)
	}
	defer scan.Free()

	if est.Type == vortex.EstimateExact {
		fmt.Printf("Total partitions to process: %d\n", est.Value)
	}

	// Extract the Arrow Schema from the Vortex Scan
	scanDType, err := scan.DType()
	if err != nil {
		log.Fatalf("Failed to get scan dtype: %v", err)
	}
	defer scanDType.Free()

	var cSchema cdata.CArrowSchema
	if err := scanDType.ToArrowSchema(unsafe.Pointer(&cSchema)); err != nil {
		log.Fatalf("Failed to export Arrow schema: %v", err)
	}

	arrowSchema, err := cdata.ImportCArrowSchema(&cSchema)
	if err != nil {
		log.Fatalf("Failed to import Arrow schema to Go: %v", err)
	}

	// Aggregate Data
	statsMap := make(map[int64]*LocationStats)
	var totalRows uint64

	// Iterate through partitions
	for {
		partition, err := scan.NextPartition()
		if err != nil {
			log.Fatalf("Error reading partition: %v", err)
		}
		if partition == nil {
			break // EOF
		}

		// Ask Vortex to dump the entire partition to an Arrow C Stream
		var cStream cdata.CArrowArrayStream
		if err := partition.ScanArrow(session, unsafe.Pointer(&cStream)); err != nil {
			log.Fatalf("Failed to scan partition to Arrow Stream: %v", err)
		}
		
		// Import the stream natively into Go using Apache Arrow
		reader, err := cdata.ImportCRecordReader(&cStream, arrowSchema)
		if err != nil {
			log.Fatalf("Failed to import C Record Reader: %v", err)
		}

		// Read batches from the stream
		for {
			rec, err := reader.Read()
			if err == io.EOF {
				break
			} else if err != nil {
				log.Fatalf("Error reading Arrow record batch: %v", err)
			}

			rows := int(rec.NumRows())
			totalRows += uint64(rows)

			// Fast-path: Extract pure Go slices that point to the underlying C memory!
			// We dynamically check for Int32/Int64 since TLC schemas vary by year.
			var locIDs32 []int32
			var locIDs64 []int64
			
			switch col := rec.Column(0).(type) {
			case *array.Int32:
				locIDs32 = col.Int32Values()
			case *array.Int64:
				locIDs64 = col.Int64Values()
			default:
				log.Fatalf("Unexpected type for PULocationID: %T", col)
			}

			dists := rec.Column(1).(*array.Float64).Float64Values()
			amts := rec.Column(2).(*array.Float64).Float64Values()

			// Highly-optimized Native Go Aggregation Loop
			if locIDs64 != nil {
				for i := 0; i < rows; i++ {
					locID := locIDs64[i]
					s, exists := statsMap[locID]
					if !exists {
						s = &LocationStats{PULocationID: locID}
						statsMap[locID] = s
					}
					s.NumTrips++
					s.TotalDistance += dists[i]
					s.TotalAmount += amts[i]
				}
			} else {
				for i := 0; i < rows; i++ {
					locID := int64(locIDs32[i])
					s, exists := statsMap[locID]
					if !exists {
						s = &LocationStats{PULocationID: locID}
						statsMap[locID] = s
					}
					s.NumTrips++
					s.TotalDistance += dists[i]
					s.TotalAmount += amts[i]
				}
			}

			// We MUST release the Arrow record so the C-memory can be safely freed
			rec.Release()
		}
	}

	// Sort and Limit the Results
	var results []*LocationStats
	for _, s := range statsMap {
		results = append(results, s)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].NumTrips > results[j].NumTrips
	})

	elapsed := time.Since(start)

	// Print Results
	fmt.Printf("\nSuccessfully aggregated %d rows in %s\n\n", totalRows, elapsed)
	fmt.Println("Top 10 Pickup Locations by Trip Count:")
	fmt.Printf("%-15s | %-12s | %-15s | %-15s\n", "PULocationID", "Num Trips", "Avg Distance", "Avg Total ($)")
	fmt.Println("----------------------------------------------------------------------")

	limit := 10
	if len(results) < 10 {
		limit = len(results)
	}

	for i := 0; i < limit; i++ {
		r := results[i]
		avgDist := r.TotalDistance / float64(r.NumTrips)
		avgTotal := r.TotalAmount / float64(r.NumTrips)

		fmt.Printf("%-15d | %-12d | %-15.2f | $%-14.2f\n",
			r.PULocationID,
			r.NumTrips,
			avgDist,
			avgTotal,
		)
	}
}
```

