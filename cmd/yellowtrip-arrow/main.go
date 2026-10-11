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
	if err := scanDType.ToArrowSchema(session, unsafe.Pointer(&cSchema)); err != nil {
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
