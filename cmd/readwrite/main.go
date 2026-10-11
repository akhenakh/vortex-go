package main

import (
	"fmt"
	"log"
	"os"

	"github.com/akhenakh/vortex-go" // Update with your actual module path
)

func main() {
	// 1. Initialize a new Vortex Session
	session := vortex.NewSession()
	defer session.Free() // Explicitly free C resources when done

	filePath := "sample.vortex"
	defer os.Remove(filePath) // Cleanup after example

	// ==========================================
	// WRITE TO VORTEX FILE
	// ==========================================

	// 2. Define the Schema: Struct { id: u32, score: f64 }
	fb := vortex.NewStructFieldsBuilder()
	if err := fb.AddField("id", vortex.NewPrimitiveDType(vortex.PTypeU32, false)); err != nil {
		log.Fatal(err)
	}
	if err := fb.AddField("score", vortex.NewPrimitiveDType(vortex.PTypeF64, false)); err != nil {
		log.Fatal(err)
	}

	// Finalize fields and create the Struct DType
	schema := vortex.NewStructDType(fb.Finalize(), false)
	defer schema.Free()

	// 3. Create Data Arrays
	val := &vortex.Validity{Type: vortex.ValidityNonNullable}

	idArr, err := vortex.NewPrimitiveArray(session, vortex.PTypeU32, []uint32{1, 2, 3}, val)
	if err != nil {
		log.Fatal(err)
	}
	defer idArr.Free()

	scoreArr, err := vortex.NewPrimitiveArray(session, vortex.PTypeF64, []float64{95.5, 82.0, 42.1}, val)
	if err != nil {
		log.Fatal(err)
	}
	defer scoreArr.Free()

	// 4. Combine into a Struct Array (Batch)
	builder := vortex.NewStructColumnBuilder(session, val, 2)
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

	// ==========================================
	// READ FROM VORTEX FILE
	// ==========================================

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
