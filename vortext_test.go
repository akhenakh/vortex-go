package vortex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSession(t *testing.T) {
	SetLogLevel(LogLevelInfo)

	session := NewSession()
	if session == nil {
		t.Fatal("Expected session to be created")
	}
	defer session.Free()

	clone := session.Clone()
	if clone == nil {
		t.Fatal("Expected session clone to be created")
	}
	defer clone.Free()
}

func TestDTypes(t *testing.T) {
	dt := NewPrimitiveDType(PTypeI32, false)
	defer dt.Free()

	if dt.Variant() != DTypePrimitive {
		t.Errorf("Expected variant DTypePrimitive, got %v", dt.Variant())
	}
	if dt.PrimitivePType() != PTypeI32 {
		t.Errorf("Expected PTypeI32, got %v", dt.PrimitivePType())
	}
	if dt.IsNullable() {
		t.Error("Expected DType to be non-nullable")
	}

	strDt := NewUTF8DType(true)
	defer strDt.Free()
	if strDt.Variant() != DTypeUTF8 {
		t.Errorf("Expected variant DTypeUTF8, got %v", strDt.Variant())
	}
	if !strDt.IsNullable() {
		t.Error("Expected DType to be nullable")
	}
}

func TestScalars(t *testing.T) {
	s := NewU32Scalar(42, false)
	defer s.Free()

	if s.IsNull() {
		t.Error("Expected scalar to not be null")
	}

	strScalar, err := NewUTF8Scalar("hello vortex", false)
	if err != nil {
		t.Fatalf("Failed to create UTF8 scalar: %v", err)
	}
	defer strScalar.Free()

	if strScalar.DType().Variant() != DTypeUTF8 {
		t.Errorf("Expected UTF8 scalar dtype")
	}
}

func TestPrimitiveArray(t *testing.T) {
	data := []uint32{10, 20, 30, 40, 50}
	val := &Validity{Type: ValidityNonNullable}

	arr, err := NewPrimitiveArray(PTypeU32, data, val)
	if err != nil {
		t.Fatalf("Failed to create array: %v", err)
	}
	defer arr.Free()

	if arr.Len() != len(data) {
		t.Errorf("Expected length %d, got %d", len(data), arr.Len())
	}

	for i, expected := range data {
		if got := arr.GetU32(i); got != expected {
			t.Errorf("At index %d: expected %d, got %d", i, expected, got)
		}
	}
}

func TestStructBuilder(t *testing.T) {
	// Create columns
	ages := []uint8{25, 30, 35}
	heights := []uint16{170, 180, 175}

	val := &Validity{Type: ValidityNonNullable}

	ageArr, err := NewPrimitiveArray(PTypeU8, ages, val)
	if err != nil {
		t.Fatal(err)
	}
	defer ageArr.Free()

	heightArr, err := NewPrimitiveArray(PTypeU16, heights, val)
	if err != nil {
		t.Fatal(err)
	}
	defer heightArr.Free()

	// Build struct
	builder := NewStructColumnBuilder(val, 2)

	err = builder.AddField("age", ageArr)
	if err != nil {
		t.Fatal(err)
	}

	err = builder.AddField("height", heightArr)
	if err != nil {
		t.Fatal(err)
	}

	structArr, err := builder.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	defer structArr.Free()

	if structArr.Len() != 3 {
		t.Errorf("Expected struct array length 3, got %d", structArr.Len())
	}

	if structArr.DType().Variant() != DTypeStruct {
		t.Errorf("Expected struct dtype")
	}
}

func TestEndToEndFileSinkAndScan(t *testing.T) {
	session := NewSession()
	defer session.Free()

	// 1. Setup a temporary file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.vortex")

	// 2. Define the Schema (Struct of u8 and u16)
	fb := NewStructFieldsBuilder()
	ageDt := NewPrimitiveDType(PTypeU8, false)
	heightDt := NewPrimitiveDType(PTypeU16, false)

	fb.AddField("age", ageDt)
	fb.AddField("height", heightDt)

	fields := fb.Finalize()

	// Create DType. NOTE: NewStructDType takes ownership of fields in C.
	structDType := NewStructDType(fields, false)
	defer structDType.Free()

	// 3. Create a Struct Array to write
	ages := []uint8{10, 20, 30, 40, 50}
	heights := []uint16{100, 120, 140, 160, 180}
	val := &Validity{Type: ValidityNonNullable}

	ageArr, _ := NewPrimitiveArray(PTypeU8, ages, val)
	heightArr, _ := NewPrimitiveArray(PTypeU16, heights, val)

	builder := NewStructColumnBuilder(val, 2)
	builder.AddField("age", ageArr)
	builder.AddField("height", heightArr)

	arrayToWrite, err := builder.Finalize()
	if err != nil {
		t.Fatalf("Failed to finalize array: %v", err)
	}
	defer arrayToWrite.Free()

	// 4. Write to File Sink
	sink, err := OpenFileSink(session, filePath, structDType)
	if err != nil {
		t.Fatalf("Failed to open sink: %v", err)
	}

	err = sink.Push(arrayToWrite)
	if err != nil {
		t.Fatalf("Failed to push to sink: %v", err)
	}

	err = sink.Close()
	if err != nil {
		t.Fatalf("Failed to close sink: %v", err)
	}

	// Make sure the file was written
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("File was not created at %s", filePath)
	}

	// 5. Read back via DataSource & Scan
	ds, err := NewDataSource(session, DataSourceOptions{Paths: filePath})
	if err != nil {
		t.Fatalf("Failed to open data source: %v", err)
	}
	defer ds.Free()

	// Ensure RowCount matches
	est := ds.RowCount()
	if est.Type != EstimateExact || est.Value != 5 {
		t.Errorf("Expected exact row count 5, got %v", est)
	}

	// 6. Execute Scan
	scan, scanEst, err := ds.Scan(nil)
	if err != nil {
		t.Fatalf("Failed to scan: %v", err)
	}
	defer scan.Free()

	if scanEst.Value != 1 {
		t.Errorf("Expected 1 partition, got %d", scanEst.Value)
	}

	// 7. Consume Partition
	partition, err := scan.NextPartition()
	if err != nil {
		t.Fatalf("Failed to get partition: %v", err)
	}
	if partition == nil {
		t.Fatal("Expected a partition, got nil")
	}
	defer partition.Free()

	// 8. Consume Array
	readArray, err := partition.Next()
	if err != nil {
		t.Fatalf("Failed to get array from partition: %v", err)
	}
	if readArray == nil {
		t.Fatal("Expected an array, got nil")
	}
	defer readArray.Free()

	if readArray.Len() != 5 {
		t.Errorf("Expected read array length 5, got %d", readArray.Len())
	}

	// 9. Extract and verify columns
	readAgeField, err := readArray.Field(0)
	if err != nil {
		t.Fatalf("Failed to extract age field: %v", err)
	}
	defer readAgeField.Free()

	if got := readAgeField.GetU8(2); got != 30 {
		t.Errorf("Expected age 30 at index 2, got %d", got)
	}
}
