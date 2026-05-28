package main

import (
	"fmt"
	"log"
	"sort"

	"github.com/akhenakh/vortex-go"
)

// LocationStats holds our aggregated data for a specific pickup location
type LocationStats struct {
	PULocationID  int32
	NumTrips      uint64
	TotalDistance float64
	TotalAmount   float64
}

func main() {
	// 1. Initialize the Vortex Session
	session := vortex.NewSession()
	defer session.Free()

	filePath := "yellow_tripdata_2024-01.vortex"
	fmt.Printf("Analyzing %s...\n", filePath)

	// 2. Open the Data Source
	ds, err := vortex.NewDataSource(session, vortex.DataSourceOptions{Paths: filePath})
	if err != nil {
		log.Fatalf("Failed to open data source: %v", err)
	}
	defer ds.Free()

	// 3. Define Projection Options
	// We don't want to load the whole file into memory.
	// We only project the 3 columns we need for our aggregation.
	root := vortex.NewRootExpression()
	defer root.Free()

	// The select expression extracts only these 3 columns
	projection := root.Select([]string{"PULocationID", "trip_distance", "total_amount"})
	defer projection.Free()

	opts := &vortex.ScanOptions{
		Projection: projection,
	}

	// 4. Start the Scan
	scan, est, err := ds.Scan(opts)
	if err != nil {
		log.Fatalf("Failed to scan: %v", err)
	}
	defer scan.Free()

	if est.Type == vortex.EstimateExact {
		rowCount := ds.RowCount()
		fmt.Printf("Total rows to process: %d\n", rowCount.Value)
	}

	// 5. Aggregate Data
	// This map will act as our `GROUP BY PULocationID`
	statsMap := make(map[int32]*LocationStats)

	// Iterate through independent partitions (chunks of the file)
	for {
		partition, err := scan.NextPartition()
		if err != nil {
			log.Fatalf("Error reading partition: %v", err)
		}
		if partition == nil {
			break // No more partitions (EOF)
		}

		// Iterate through the arrays (batches) within the partition
		for {
			arr, err := partition.Next()
			if err != nil {
				log.Fatalf("Error reading array batch: %v", err)
			}
			if arr == nil {
				break // End of partition
			}

			// Extract our 3 projected columns
			// (Order matches the projection string slice above)
			locField, _ := arr.Field(0)  // PULocationID
			distField, _ := arr.Field(1) // trip_distance
			amtField, _ := arr.Field(2)  // total_amount

			length := arr.Len()

			// Perform the aggregation for this batch
			for i := 0; i < length; i++ {
				// Note: Types here depend on how the dataset was encoded.
				// TLC data typically uses int32 for IDs and float64 for distances/amounts.
				locID := locField.GetI32(i)
				dist := distField.GetF64(i)
				amt := amtField.GetF64(i)

				s, exists := statsMap[locID]
				if !exists {
					s = &LocationStats{PULocationID: locID}
					statsMap[locID] = s
				}
				s.NumTrips++
				s.TotalDistance += dist
				s.TotalAmount += amt
			}

			// Manually free fields and arrays inside loops to prevent memory pressure
			// before the Go garbage collector gets around to finalizing them.
			locField.Free()
			distField.Free()
			amtField.Free()
			arr.Free()
		}
		partition.Free()
	}

	// 6. Sort and Limit the Results
	// (Equivalent to: ORDER BY num_trips DESC LIMIT 10)
	var results []*LocationStats
	for _, s := range statsMap {
		results = append(results, s)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].NumTrips > results[j].NumTrips
	})

	// Print the Top 10 Locations
	fmt.Println("\nTop 10 Pickup Locations by Trip Count:")
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
