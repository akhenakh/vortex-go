//go:generate cd ../vortex-src && cargo build -p vortex-ffi --release
package vortex

/*
// Point to the header file inside the submodule
#cgo CFLAGS: -I${SRCDIR}/vortex-src/vortex-ffi/cinclude

// Point to the compiled static library inside the submodule
#cgo LDFLAGS: ${SRCDIR}/vortex-src/target/release/libvortex_ffi.a

// OS-Specific System Dependencies
// Rust staticlibs require the host language to link system dependencies.

// Linux dependencies (Math, Threading, Dynamic Linking)
#cgo linux LDFLAGS: -lm -lpthread -ldl

// macOS dependencies (CoreFoundation and Security are required by
// rust-tls/reqwest used in the object_store crate for S3/GCP/Azure)
#cgo darwin LDFLAGS: -framework CoreFoundation -framework Security -framework SystemConfiguration

// Windows dependencies (User environment, Sockets, Cryptography)
#cgo windows LDFLAGS: -lws2_32 -luserenv -lbcrypt

#include "vortex.h"
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

// Enums

type DTypeVariant uint32

const (
	DTypeNull          DTypeVariant = C.DTYPE_NULL
	DTypeBool          DTypeVariant = C.DTYPE_BOOL
	DTypePrimitive     DTypeVariant = C.DTYPE_PRIMITIVE
	DTypeUTF8          DTypeVariant = C.DTYPE_UTF8
	DTypeBinary        DTypeVariant = C.DTYPE_BINARY
	DTypeStruct        DTypeVariant = C.DTYPE_STRUCT
	DTypeList          DTypeVariant = C.DTYPE_LIST
	DTypeExtension     DTypeVariant = C.DTYPE_EXTENSION
	DTypeDecimal       DTypeVariant = C.DTYPE_DECIMAL
	DTypeFixedSizeList DTypeVariant = C.DTYPE_FIXED_SIZE_LIST
)

type PType uint32

const (
	PTypeU8  PType = C.PTYPE_U8
	PTypeU16 PType = C.PTYPE_U16
	PTypeU32 PType = C.PTYPE_U32
	PTypeU64 PType = C.PTYPE_U64
	PTypeI8  PType = C.PTYPE_I8
	PTypeI16 PType = C.PTYPE_I16
	PTypeI32 PType = C.PTYPE_I32
	PTypeI64 PType = C.PTYPE_I64
	PTypeF16 PType = C.PTYPE_F16
	PTypeF32 PType = C.PTYPE_F32
	PTypeF64 PType = C.PTYPE_F64
)

type ValidityType uint32

const (
	ValidityNonNullable ValidityType = C.VX_VALIDITY_NON_NULLABLE
	ValidityAllValid    ValidityType = C.VX_VALIDITY_ALL_VALID
	ValidityAllInvalid  ValidityType = C.VX_VALIDITY_ALL_INVALID
	ValidityArray       ValidityType = C.VX_VALIDITY_ARRAY
)

type EstimateType uint32

const (
	EstimateUnknown EstimateType = C.VX_ESTIMATE_UNKNOWN
	EstimateExact   EstimateType = C.VX_ESTIMATE_EXACT
	EstimateInexact EstimateType = C.VX_ESTIMATE_INEXACT
)

type BinaryOperator uint32

const (
	OperatorEq        BinaryOperator = C.VX_OPERATOR_EQ
	OperatorNotEq     BinaryOperator = C.VX_OPERATOR_NOT_EQ
	OperatorGt        BinaryOperator = C.VX_OPERATOR_GT
	OperatorGte       BinaryOperator = C.VX_OPERATOR_GTE
	OperatorLt        BinaryOperator = C.VX_OPERATOR_LT
	OperatorLte       BinaryOperator = C.VX_OPERATOR_LTE
	OperatorKleeneAnd BinaryOperator = C.VX_OPERATOR_KLEENE_AND
	OperatorKleeneOr  BinaryOperator = C.VX_OPERATOR_KLEENE_OR
	OperatorAdd       BinaryOperator = C.VX_OPERATOR_ADD
	OperatorSub       BinaryOperator = C.VX_OPERATOR_SUB
	OperatorMul       BinaryOperator = C.VX_OPERATOR_MUL
	OperatorDiv       BinaryOperator = C.VX_OPERATOR_DIV
)

type LogLevel uint32

const (
	LogLevelOff   LogLevel = C.LOG_LEVEL_OFF
	LogLevelError LogLevel = C.LOG_LEVEL_ERROR
	LogLevelWarn  LogLevel = C.LOG_LEVEL_WARN
	LogLevelInfo  LogLevel = C.LOG_LEVEL_INFO
	LogLevelDebug LogLevel = C.LOG_LEVEL_DEBUG
	LogLevelTrace LogLevel = C.LOG_LEVEL_TRACE
)

type ScanSelectionInclude uint32

const (
	SelectionIncludeAll   ScanSelectionInclude = C.VX_SELECTION_INCLUDE_ALL
	SelectionIncludeRange ScanSelectionInclude = C.VX_SELECTION_INCLUDE_RANGE
	SelectionExcludeRange ScanSelectionInclude = C.VX_SELECTION_EXCLUDE_RANGE
)

func SetLogLevel(level LogLevel) {
	C.vx_set_log_level(C.vx_log_level(level))
}

// Errors
func checkError(err *C.vx_error) error {
	if err == nil {
		return nil
	}
	defer C.vx_error_free(err)
	msgStr := C.vx_error_get_message(err)
	msg := C.GoStringN(C.vx_string_ptr(msgStr), C.int(C.vx_string_len(msgStr)))
	return fmt.Errorf("vortex: %s", msg)
}

// Internal Helpers

func cStringList(strs []string) (**C.char, func()) {
	if len(strs) == 0 {
		return nil, func() {}
	}
	cStrs := make([]*C.char, len(strs))
	for i, s := range strs {
		cStrs[i] = C.CString(s)
	}
	return (**C.char)(unsafe.Pointer(&cStrs[0])), func() {
		for _, c := range cStrs {
			C.free(unsafe.Pointer(c))
		}
	}
}

// Session

type Session struct {
	c *C.vx_session
}

func NewSession() *Session {
	s := &Session{c: C.vx_session_new()}
	runtime.SetFinalizer(s, (*Session).Free)
	return s
}

func (s *Session) Clone() *Session {
	clone := &Session{c: C.vx_session_clone(s.c)}
	runtime.SetFinalizer(clone, (*Session).Free)
	return clone
}

func (s *Session) Free() {
	if s.c != nil {
		C.vx_session_free(s.c)
		s.c = nil
		runtime.SetFinalizer(s, nil)
	}
}

// DType
type DType struct {
	c     *C.vx_dtype
	owned bool
}

func wrapDType(c *C.vx_dtype, owned bool) *DType {
	if c == nil {
		return nil
	}
	dt := &DType{c: c, owned: owned}
	if owned {
		runtime.SetFinalizer(dt, (*DType).Free)
	}
	return dt
}

func (d *DType) Free() {
	if d.owned && d.c != nil {
		C.vx_dtype_free(d.c)
		d.c = nil
		runtime.SetFinalizer(d, nil)
	}
}

func (d *DType) Clone() *DType {
	return wrapDType(C.vx_dtype_clone(d.c), true)
}

func NewNullDType() *DType { return wrapDType(C.vx_dtype_new_null(), true) }
func NewBoolDType(nullable bool) *DType {
	return wrapDType(C.vx_dtype_new_bool(C.bool(nullable)), true)
}
func NewPrimitiveDType(p PType, nullable bool) *DType {
	return wrapDType(C.vx_dtype_new_primitive(C.vx_ptype(p), C.bool(nullable)), true)
}
func NewUTF8DType(nullable bool) *DType {
	return wrapDType(C.vx_dtype_new_utf8(C.bool(nullable)), true)
}
func NewBinaryDType(nullable bool) *DType {
	return wrapDType(C.vx_dtype_new_binary(C.bool(nullable)), true)
}

func NewListDType(element *DType, nullable bool) *DType {
	// C takes ownership of the element pointer, so we clone without wrapping in a Go object
	cElem := C.vx_dtype_clone(element.c)
	return wrapDType(C.vx_dtype_new_list(cElem, C.bool(nullable)), true)
}
func NewFixedSizeListDType(element *DType, size uint32, nullable bool) *DType {
	cElem := C.vx_dtype_clone(element.c)
	return wrapDType(C.vx_dtype_new_fixed_size_list(cElem, C.uint32_t(size), C.bool(nullable)), true)
}
func NewStructDType(fields *StructFields, nullable bool) *DType {
	// Consumes fields builder in C. We must disable Go's finalizer so it isn't double-freed.
	dt := wrapDType(C.vx_dtype_new_struct(fields.c, C.bool(nullable)), true)
	fields.c = nil
	runtime.SetFinalizer(fields, nil)
	return dt
}
func NewDecimalDType(precision uint8, scale int8, nullable bool) *DType {
	return wrapDType(C.vx_dtype_new_decimal(C.uint8_t(precision), C.int8_t(scale), C.bool(nullable)), true)
}

func (d *DType) Variant() DTypeVariant   { return DTypeVariant(C.vx_dtype_get_variant(d.c)) }
func (d *DType) IsNullable() bool        { return bool(C.vx_dtype_is_nullable(d.c)) }
func (d *DType) PrimitivePType() PType   { return PType(C.vx_dtype_primitive_ptype(d.c)) }
func (d *DType) DecimalPrecision() uint8 { return uint8(C.vx_dtype_decimal_precision(d.c)) }
func (d *DType) DecimalScale() int8      { return int8(C.vx_dtype_decimal_scale(d.c)) }
func (d *DType) StructFields() *StructFields {
	return wrapStructFields(C.vx_dtype_struct_dtype(d.c), false)
}
func (d *DType) ListElement() *DType { return wrapDType(C.vx_dtype_list_element(d.c), false) }
func (d *DType) FixedSizeListElement() *DType {
	return wrapDType(C.vx_dtype_fixed_size_list_element(d.c), false)
}
func (d *DType) FixedSizeListSize() uint32 { return uint32(C.vx_dtype_fixed_size_list_size(d.c)) }
func (d *DType) IsTime() bool              { return bool(C.vx_dtype_is_time((*C.DType)(d.c))) }
func (d *DType) IsDate() bool              { return bool(C.vx_dtype_is_date((*C.DType)(d.c))) }
func (d *DType) IsTimestamp() bool         { return bool(C.vx_dtype_is_timestamp((*C.DType)(d.c))) }
func (d *DType) TimeUnit() uint8           { return uint8(C.vx_dtype_time_unit((*C.DType)(d.c))) }
func (d *DType) TimeZone() string {
	tz := C.vx_dtype_time_zone((*C.DType)(d.c))
	if tz == nil {
		return ""
	}
	defer C.vx_string_free(tz)
	return C.GoStringN(C.vx_string_ptr(tz), C.int(C.vx_string_len(tz)))
}

// Arrow C-Data Interfaces
func (d *DType) ToArrowSchema(schema unsafe.Pointer) error {
	var cErr *C.vx_error
	if C.vx_dtype_to_arrow_schema(d.c, (*C.FFI_ArrowSchema)(schema), &cErr) != 0 {
		return checkError(cErr)
	}
	return nil
}

func DTypeFromArrowSchema(schema unsafe.Pointer) (*DType, error) {
	var cErr *C.vx_error
	dt := C.vx_dtype_from_arrow_schema((*C.FFI_ArrowSchema)(schema), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapDType(dt, true), nil
}

// StructFields & Builder
type StructFields struct {
	c     *C.vx_struct_fields
	owned bool
}

func wrapStructFields(c *C.vx_struct_fields, owned bool) *StructFields {
	if c == nil {
		return nil
	}
	sf := &StructFields{c: c, owned: owned}
	if owned {
		runtime.SetFinalizer(sf, (*StructFields).Free)
	}
	return sf
}

func (s *StructFields) Free() {
	if s.owned && s.c != nil {
		C.vx_struct_fields_free(s.c)
		s.c = nil
		runtime.SetFinalizer(s, nil)
	}
}

func (s *StructFields) NFields() uint64 {
	return uint64(C.vx_struct_fields_nfields(s.c))
}

func (s *StructFields) FieldName(idx int) string {
	str := C.vx_struct_fields_field_name(s.c, C.size_t(idx))
	if str == nil {
		return ""
	}
	return C.GoStringN(C.vx_string_ptr(str), C.int(C.vx_string_len(str)))
}

func (s *StructFields) FieldDType(idx int) *DType {
	return wrapDType(C.vx_struct_fields_field_dtype(s.c, C.size_t(idx)), true) // C function returns owned reference
}

type StructFieldsBuilder struct {
	c *C.vx_struct_fields_builder
}

func NewStructFieldsBuilder() *StructFieldsBuilder {
	b := &StructFieldsBuilder{c: C.vx_struct_fields_builder_new()}
	runtime.SetFinalizer(b, (*StructFieldsBuilder).Free)
	return b
}

func (b *StructFieldsBuilder) Free() {
	if b.c != nil {
		C.vx_struct_fields_builder_free(b.c)
		b.c = nil
		runtime.SetFinalizer(b, nil)
	}
}

func (b *StructFieldsBuilder) AddField(name string, dtype *DType) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	// Create vx string, do NOT defer free because C takes ownership
	vxName := C.vx_string_new(cName, C.size_t(len(name)))

	// Create raw C clone of dtype, do NOT defer free because C takes ownership
	cClone := C.vx_dtype_clone(dtype.c)

	C.vx_struct_fields_builder_add_field(b.c, vxName, cClone)
}

func (b *StructFieldsBuilder) Finalize() *StructFields {
	fields := C.vx_struct_fields_builder_finalize(b.c)
	b.c = nil // Finalize consumes builder in C
	runtime.SetFinalizer(b, nil)
	return wrapStructFields(fields, true)
}

// Scalar

type Scalar struct {
	c     *C.vx_scalar
	owned bool
}

func wrapScalar(c *C.vx_scalar, owned bool) *Scalar {
	if c == nil {
		return nil
	}
	s := &Scalar{c: c, owned: owned}
	if owned {
		runtime.SetFinalizer(s, (*Scalar).Free)
	}
	return s
}

func (s *Scalar) Free() {
	if s.owned && s.c != nil {
		C.vx_scalar_free(s.c)
		s.c = nil
		runtime.SetFinalizer(s, nil)
	}
}

func (s *Scalar) Clone() *Scalar {
	return wrapScalar(C.vx_scalar_clone(s.c), true)
}

func (s *Scalar) DType() *DType {
	return wrapDType(C.vx_scalar_dtype(s.c), false)
}

func (s *Scalar) IsNull() bool {
	return bool(C.vx_scalar_is_null(s.c))
}

func NewBoolScalar(val, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_bool(C.bool(val), C.bool(nullable)), true)
}
func NewU8Scalar(val uint8, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_u8(C.uint8_t(val), C.bool(nullable)), true)
}
func NewU16Scalar(val uint16, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_u16(C.uint16_t(val), C.bool(nullable)), true)
}
func NewU32Scalar(val uint32, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_u32(C.uint32_t(val), C.bool(nullable)), true)
}
func NewU64Scalar(val uint64, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_u64(C.uint64_t(val), C.bool(nullable)), true)
}
func NewI8Scalar(val int8, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_i8(C.int8_t(val), C.bool(nullable)), true)
}
func NewI16Scalar(val int16, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_i16(C.int16_t(val), C.bool(nullable)), true)
}
func NewI32Scalar(val int32, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_i32(C.int32_t(val), C.bool(nullable)), true)
}
func NewI64Scalar(val int64, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_i64(C.int64_t(val), C.bool(nullable)), true)
}
func NewF32Scalar(val float32, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_f32(C.float(val), C.bool(nullable)), true)
}
func NewF64Scalar(val float64, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_f64(C.double(val), C.bool(nullable)), true)
}
func NewF16BitsScalar(bits uint16, nullable bool) *Scalar {
	return wrapScalar(C.vx_scalar_new_f16_bits(C.uint16_t(bits), C.bool(nullable)), true)
}

func NewUTF8Scalar(val string, nullable bool) (*Scalar, error) {
	cStr := C.CString(val)
	defer C.free(unsafe.Pointer(cStr))
	var cErr *C.vx_error
	c := C.vx_scalar_new_utf8(cStr, C.size_t(len(val)), C.bool(nullable), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapScalar(c, true), nil
}

func NewBinaryScalar(val []byte, nullable bool) (*Scalar, error) {
	var ptr *C.uint8_t
	if len(val) > 0 {
		ptr = (*C.uint8_t)(unsafe.Pointer(&val[0]))
	}
	var cErr *C.vx_error
	c := C.vx_scalar_new_binary(ptr, C.size_t(len(val)), C.bool(nullable), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapScalar(c, true), nil
}

func NewNullScalar(dtype *DType) (*Scalar, error) {
	var cErr *C.vx_error
	c := C.vx_scalar_new_null(dtype.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapScalar(c, true), nil
}

func NewListScalar(elementDtype *DType, elements []*Scalar, nullable bool) (*Scalar, error) {
	var ptr **C.vx_scalar
	if len(elements) > 0 {
		cElems := make([]*C.vx_scalar, len(elements))
		for i, s := range elements {
			cElems[i] = s.c
		}
		ptr = (**C.vx_scalar)(unsafe.Pointer(&cElems[0]))
	}
	var cErr *C.vx_error
	c := C.vx_scalar_new_list(elementDtype.c, ptr, C.size_t(len(elements)), C.bool(nullable), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapScalar(c, true), nil
}

func NewFixedSizeListScalar(elementDtype *DType, elements []*Scalar, nullable bool) (*Scalar, error) {
	var ptr **C.vx_scalar
	if len(elements) > 0 {
		cElems := make([]*C.vx_scalar, len(elements))
		for i, s := range elements {
			cElems[i] = s.c
		}
		ptr = (**C.vx_scalar)(unsafe.Pointer(&cElems[0]))
	}
	var cErr *C.vx_error
	c := C.vx_scalar_new_fixed_size_list(elementDtype.c, ptr, C.size_t(len(elements)), C.bool(nullable), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapScalar(c, true), nil
}

func NewStructScalar(structDType *DType, fields []*Scalar) (*Scalar, error) {
	var ptr **C.vx_scalar
	if len(fields) > 0 {
		cElems := make([]*C.vx_scalar, len(fields))
		for i, s := range fields {
			cElems[i] = s.c
		}
		ptr = (**C.vx_scalar)(unsafe.Pointer(&cElems[0]))
	}
	var cErr *C.vx_error
	c := C.vx_scalar_new_struct(structDType.c, ptr, C.size_t(len(fields)), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapScalar(c, true), nil
}

// Expression

type Expression struct {
	c     *C.vx_expression
	owned bool
}

func wrapExpression(c *C.vx_expression, owned bool) *Expression {
	if c == nil {
		return nil
	}
	e := &Expression{c: c, owned: owned}
	if owned {
		runtime.SetFinalizer(e, (*Expression).Free)
	}
	return e
}

func (e *Expression) Free() {
	if e.owned && e.c != nil {
		C.vx_expression_free(e.c)
		e.c = nil
		runtime.SetFinalizer(e, nil)
	}
}

func NewRootExpression() *Expression {
	return wrapExpression(C.vx_expression_root(), true)
}

func NewLiteralExpression(scalar *Scalar) (*Expression, error) {
	var cErr *C.vx_error
	c := C.vx_expression_literal(scalar.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapExpression(c, true), nil
}

func (e *Expression) Select(names []string) *Expression {
	cNames, freeFn := cStringList(names)
	defer freeFn()
	c := C.vx_expression_select(cNames, C.size_t(len(names)), e.c)
	return wrapExpression(c, true)
}

func ExpressionAnd(expressions []*Expression) *Expression {
	var ptr **C.vx_expression
	if len(expressions) > 0 {
		cExprs := make([]*C.vx_expression, len(expressions))
		for i, ex := range expressions {
			cExprs[i] = ex.c
		}
		ptr = (**C.vx_expression)(unsafe.Pointer(&cExprs[0]))
	}
	c := C.vx_expression_and(ptr, C.size_t(len(expressions)))
	return wrapExpression(c, true)
}

func ExpressionOr(expressions []*Expression) *Expression {
	var ptr **C.vx_expression
	if len(expressions) > 0 {
		cExprs := make([]*C.vx_expression, len(expressions))
		for i, ex := range expressions {
			cExprs[i] = ex.c
		}
		ptr = (**C.vx_expression)(unsafe.Pointer(&cExprs[0]))
	}
	c := C.vx_expression_or(ptr, C.size_t(len(expressions)))
	return wrapExpression(c, true)
}

func ExpressionBinary(operator BinaryOperator, lhs, rhs *Expression) *Expression {
	c := C.vx_expression_binary(C.vx_binary_operator(operator), lhs.c, rhs.c)
	return wrapExpression(c, true)
}

func (e *Expression) Not() *Expression {
	return wrapExpression(C.vx_expression_not(e.c), true)
}

func (e *Expression) IsNull() *Expression {
	return wrapExpression(C.vx_expression_is_null(e.c), true)
}

func (e *Expression) GetItem(item string) *Expression {
	cItem := C.CString(item)
	defer C.free(unsafe.Pointer(cItem))
	return wrapExpression(C.vx_expression_get_item(cItem, e.c), true)
}

func (e *Expression) ListContains(value *Expression) *Expression {
	return wrapExpression(C.vx_expression_list_contains(e.c, value.c), true)
}

// Validity

type Validity struct {
	Type  ValidityType
	Array *Array
}

func (v *Validity) toC() C.vx_validity {
	var arr *C.vx_array
	if v != nil && v.Array != nil {
		arr = v.Array.c
	}
	typ := ValidityNonNullable
	if v != nil {
		typ = v.Type
	}
	return C.vx_validity{
		_type: C.vx_validity_type(typ),
		array: arr,
	}
}

// Array

type Array struct {
	c     *C.vx_array
	owned bool
}

func wrapArray(c *C.vx_array, owned bool) *Array {
	if c == nil {
		return nil
	}
	a := &Array{c: c, owned: owned}
	if owned {
		runtime.SetFinalizer(a, (*Array).Free)
	}
	return a
}

func (a *Array) Free() {
	if a.owned && a.c != nil {
		C.vx_array_free(a.c)
		a.c = nil
		runtime.SetFinalizer(a, nil)
	}
}

func (a *Array) Clone() *Array {
	return wrapArray(C.vx_array_clone(a.c), true)
}

func (a *Array) IsNullable() bool { return bool(C.vx_array_is_nullable(a.c)) }
func (a *Array) HasDType(variant DTypeVariant) bool {
	return bool(C.vx_array_has_dtype(a.c, C.vx_dtype_variant(variant)))
}
func (a *Array) IsPrimitive(ptype PType) bool {
	return bool(C.vx_array_is_primitive(a.c, C.vx_ptype(ptype)))
}
func (a *Array) Len() int { return int(C.vx_array_len(a.c)) }

func (a *Array) DType() *DType {
	return wrapDType(C.vx_array_dtype(a.c), false)
}

func (a *Array) Validity() (*Validity, error) {
	var cErr *C.vx_error
	var cVal C.vx_validity
	C.vx_array_get_validity(a.c, &cVal, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return &Validity{
		Type:  ValidityType(cVal._type),
		Array: wrapArray(cVal.array, true),
	}, nil
}

func (a *Array) Field(index int) (*Array, error) {
	var cErr *C.vx_error
	c := C.vx_array_get_field(a.c, C.size_t(index), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

func (a *Array) Slice(start, stop int) (*Array, error) {
	var cErr *C.vx_error
	c := C.vx_array_slice(a.c, C.size_t(start), C.size_t(stop), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

func (a *Array) ElementIsInvalid(index int) (bool, error) {
	var cErr *C.vx_error
	res := C.vx_array_element_is_invalid(a.c, C.size_t(index), &cErr)
	if err := checkError(cErr); err != nil {
		return false, err
	}
	return bool(res), nil
}

func (a *Array) InvalidCount() (int, error) {
	var cErr *C.vx_error
	res := C.vx_array_invalid_count(a.c, &cErr)
	if err := checkError(cErr); err != nil {
		return 0, err
	}
	return int(res), nil
}

func NewNullArray(len int) *Array {
	return wrapArray(C.vx_array_new_null(C.size_t(len)), true)
}

// NewPrimitiveArray accepts a generic slice. It copies the underlying data.
func NewPrimitiveArray[T any](ptype PType, data []T, validity *Validity) (*Array, error) {
	var cErr *C.vx_error
	cVal := validity.toC()

	var ptr unsafe.Pointer
	if len(data) > 0 {
		ptr = unsafe.Pointer(&data[0])
	}
	c := C.vx_array_new_primitive(C.vx_ptype(ptype), ptr, C.size_t(len(data)), &cVal, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

func ArrayFromArrow(arrowArray, arrowSchema unsafe.Pointer, nullable bool) (*Array, error) {
	var cErr *C.vx_error
	c := C.vx_array_from_arrow((*C.FFI_ArrowArray)(arrowArray), (*C.FFI_ArrowSchema)(arrowSchema), C.bool(nullable), &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

func (a *Array) GetU8(index int) uint8    { return uint8(C.vx_array_get_u8(a.c, C.size_t(index))) }
func (a *Array) GetU16(index int) uint16  { return uint16(C.vx_array_get_u16(a.c, C.size_t(index))) }
func (a *Array) GetU32(index int) uint32  { return uint32(C.vx_array_get_u32(a.c, C.size_t(index))) }
func (a *Array) GetU64(index int) uint64  { return uint64(C.vx_array_get_u64(a.c, C.size_t(index))) }
func (a *Array) GetI8(index int) int8     { return int8(C.vx_array_get_i8(a.c, C.size_t(index))) }
func (a *Array) GetI16(index int) int16   { return int16(C.vx_array_get_i16(a.c, C.size_t(index))) }
func (a *Array) GetI32(index int) int32   { return int32(C.vx_array_get_i32(a.c, C.size_t(index))) }
func (a *Array) GetI64(index int) int64   { return int64(C.vx_array_get_i64(a.c, C.size_t(index))) }
func (a *Array) GetF16(index int) uint16  { return uint16(C.vx_array_get_f16(a.c, C.size_t(index))) }
func (a *Array) GetF32(index int) float32 { return float32(C.vx_array_get_f32(a.c, C.size_t(index))) }
func (a *Array) GetF64(index int) float64 { return float64(C.vx_array_get_f64(a.c, C.size_t(index))) }

func (a *Array) GetStorageU8(index int) uint8 {
	return uint8(C.vx_array_get_storage_u8(a.c, C.size_t(index)))
}
func (a *Array) GetStorageU16(index int) uint16 {
	return uint16(C.vx_array_get_storage_u16(a.c, C.size_t(index)))
}
func (a *Array) GetStorageU32(index int) uint32 {
	return uint32(C.vx_array_get_storage_u32(a.c, C.size_t(index)))
}
func (a *Array) GetStorageU64(index int) uint64 {
	return uint64(C.vx_array_get_storage_u64(a.c, C.size_t(index)))
}
func (a *Array) GetStorageI8(index int) int8 {
	return int8(C.vx_array_get_storage_i8(a.c, C.size_t(index)))
}
func (a *Array) GetStorageI16(index int) int16 {
	return int16(C.vx_array_get_storage_i16(a.c, C.size_t(index)))
}
func (a *Array) GetStorageI32(index int) int32 {
	return int32(C.vx_array_get_storage_i32(a.c, C.size_t(index)))
}
func (a *Array) GetStorageI64(index int) int64 {
	return int64(C.vx_array_get_storage_i64(a.c, C.size_t(index)))
}
func (a *Array) GetStorageF16(index int) uint16 {
	return uint16(C.vx_array_get_storage_f16(a.c, C.size_t(index)))
}
func (a *Array) GetStorageF32(index int) float32 {
	return float32(C.vx_array_get_storage_f32(a.c, C.size_t(index)))
}
func (a *Array) GetStorageF64(index int) float64 {
	return float64(C.vx_array_get_storage_f64(a.c, C.size_t(index)))
}

func (a *Array) GetUTF8(index uint32) string {
	vxStr := C.vx_array_get_utf8(a.c, C.uint32_t(index))
	if vxStr == nil {
		return ""
	}
	defer C.vx_string_free(vxStr)
	return C.GoStringN(C.vx_string_ptr(vxStr), C.int(C.vx_string_len(vxStr)))
}

func (a *Array) GetBinary(index uint32) []byte {
	vxBin := C.vx_array_get_binary(a.c, C.uint32_t(index))
	if vxBin == nil {
		return nil
	}
	defer C.vx_binary_free(vxBin)
	length := C.vx_binary_len(vxBin)
	if length == 0 {
		return []byte{}
	}
	return C.GoBytes(unsafe.Pointer(C.vx_binary_ptr(vxBin)), C.int(length))
}

func (a *Array) Apply(expr *Expression) (*Array, error) {
	var cErr *C.vx_error
	c := C.vx_array_apply(a.c, expr.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

// ArrayIterator
type ArrayIterator struct {
	c *C.vx_array_iterator
}

func (i *ArrayIterator) Free() {
	if i.c != nil {
		C.vx_array_iterator_free(i.c)
		i.c = nil
		runtime.SetFinalizer(i, nil)
	}
}

func (i *ArrayIterator) Next() (*Array, error) {
	var cErr *C.vx_error
	c := C.vx_array_iterator_next(i.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

// StructColumnBuilder
type StructColumnBuilder struct {
	c *C.vx_struct_column_builder
}

func NewStructColumnBuilder(validity *Validity, capacity int) *StructColumnBuilder {
	cVal := validity.toC()
	b := &StructColumnBuilder{c: C.vx_struct_column_builder_new(&cVal, C.size_t(capacity))}
	runtime.SetFinalizer(b, (*StructColumnBuilder).Free)
	return b
}

func (b *StructColumnBuilder) Free() {
	if b.c != nil {
		C.vx_struct_column_builder_free(b.c)
		b.c = nil
		runtime.SetFinalizer(b, nil)
	}
}

func (b *StructColumnBuilder) AddField(name string, array *Array) error {
	var cErr *C.vx_error
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	C.vx_struct_column_builder_add_field(b.c, cName, array.c, &cErr)
	return checkError(cErr)
}

func (b *StructColumnBuilder) Finalize() (*Array, error) {
	var cErr *C.vx_error
	c := C.vx_struct_column_builder_finalize(b.c, &cErr)
	b.c = nil // Finalize consumes builder
	runtime.SetFinalizer(b, nil)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

// ArraySink
type ArraySink struct {
	c *C.vx_array_sink
}

func OpenFileSink(session *Session, path string, dtype *DType) (*ArraySink, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var cErr *C.vx_error
	c := C.vx_array_sink_open_file(session.c, cPath, dtype.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return &ArraySink{c: c}, nil
}

func (s *ArraySink) Push(array *Array) error {
	var cErr *C.vx_error
	C.vx_array_sink_push(s.c, array.c, &cErr)
	return checkError(cErr)
}

func (s *ArraySink) Close() error {
	var cErr *C.vx_error
	C.vx_array_sink_close(s.c, &cErr)
	s.c = nil
	return checkError(cErr)
}

// Data Source & Scan
type DataSource struct {
	c *C.vx_data_source
}

func (ds *DataSource) Free() {
	if ds.c != nil {
		C.vx_data_source_free(ds.c)
		ds.c = nil
		runtime.SetFinalizer(ds, nil)
	}
}

func (ds *DataSource) Clone() *DataSource {
	clone := &DataSource{c: C.vx_data_source_clone(ds.c)}
	runtime.SetFinalizer(clone, (*DataSource).Free)
	return clone
}

type DataSourceOptions struct {
	Paths string
}

func NewDataSource(session *Session, opts DataSourceOptions) (*DataSource, error) {
	cPaths := C.CString(opts.Paths)
	defer C.free(unsafe.Pointer(cPaths))

	cOpts := C.vx_data_source_options{paths: cPaths}
	var cErr *C.vx_error
	c := C.vx_data_source_new(session.c, &cOpts, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	ds := &DataSource{c: c}
	runtime.SetFinalizer(ds, (*DataSource).Free)
	return ds, nil
}

func (ds *DataSource) DType() *DType {
	return wrapDType(C.vx_data_source_dtype(ds.c), false)
}

type Estimate struct {
	Type  EstimateType
	Value uint64
}

func (ds *DataSource) RowCount() Estimate {
	var cEst C.vx_estimate
	C.vx_data_source_get_row_count(ds.c, &cEst)
	return Estimate{
		Type:  EstimateType(cEst._type),
		Value: uint64(cEst.estimate),
	}
}

type ScanSelection struct {
	Include ScanSelectionInclude
	Indices []uint64
}

type ScanOptions struct {
	Projection    *Expression
	Filter        *Expression
	RowRangeBegin uint64
	RowRangeEnd   uint64
	Selection     *ScanSelection
	Limit         uint64
	MaxThreads    uint64
	Ordered       bool
}

type Scan struct {
	c *C.vx_scan
}

func (s *Scan) Free() {
	if s.c != nil {
		C.vx_scan_free(s.c)
		s.c = nil
		runtime.SetFinalizer(s, nil)
	}
}

func (ds *DataSource) Scan(opts *ScanOptions) (*Scan, *Estimate, error) {
	var cOpts *C.vx_scan_options
	var cOptsVal C.vx_scan_options

	if opts != nil {
		cOpts = &cOptsVal
		if opts.Projection != nil {
			cOpts.projection = opts.Projection.c
		}
		if opts.Filter != nil {
			cOpts.filter = opts.Filter.c
		}
		cOpts.row_range_begin = C.uint64_t(opts.RowRangeBegin)
		cOpts.row_range_end = C.uint64_t(opts.RowRangeEnd)
		cOpts.limit = C.uint64_t(opts.Limit)
		cOpts.max_threads = C.uint64_t(opts.MaxThreads)
		cOpts.ordered = C.bool(opts.Ordered)

		if opts.Selection != nil {
			cOpts.selection.include = C.vx_scan_selection_include(opts.Selection.Include)
			if len(opts.Selection.Indices) > 0 {
				cOpts.selection.idx = (*C.uint64_t)(&opts.Selection.Indices[0])
				cOpts.selection.idx_len = C.size_t(len(opts.Selection.Indices))
			}
		}
	}

	var cErr *C.vx_error
	var cEst C.vx_estimate
	c := C.vx_data_source_scan(ds.c, cOpts, &cEst, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, nil, err
	}

	est := &Estimate{
		Type:  EstimateType(cEst._type),
		Value: uint64(cEst.estimate),
	}
	s := &Scan{c: c}
	runtime.SetFinalizer(s, (*Scan).Free)
	return s, est, nil
}

func (s *Scan) DType() (*DType, error) {
	var cErr *C.vx_error
	dt := C.vx_scan_dtype(s.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapDType(dt, false), nil
}

func (s *Scan) NextPartition() (*Partition, error) {
	var cErr *C.vx_error
	p := C.vx_scan_next_partition(s.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil // Exhausted
	}
	part := &Partition{c: p}
	runtime.SetFinalizer(part, (*Partition).Free)
	return part, nil
}

type Partition struct {
	c *C.vx_partition
}

func (p *Partition) Free() {
	if p.c != nil {
		C.vx_partition_free(p.c)
		p.c = nil
		runtime.SetFinalizer(p, nil)
	}
}

func (p *Partition) RowCount() (*Estimate, error) {
	var cErr *C.vx_error
	var cEst C.vx_estimate
	C.vx_partition_row_count(p.c, &cEst, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return &Estimate{
		Type:  EstimateType(cEst._type),
		Value: uint64(cEst.estimate),
	}, nil
}

func (p *Partition) Next() (*Array, error) {
	var cErr *C.vx_error
	c := C.vx_partition_next(p.c, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapArray(c, true), nil
}

func (p *Partition) ScanArrow(session *Session, stream unsafe.Pointer) error {
	var cErr *C.vx_error
	C.vx_partition_scan_arrow(session.c, p.c, (*C.FFI_ArrowArrayStream)(stream), &cErr)

	// Consumes the partition fully, cancel the finalizer
	p.c = nil
	runtime.SetFinalizer(p, nil)

	return checkError(cErr)
}

// File
type File struct {
	c *C.vx_file
}

func wrapFile(c *C.vx_file) *File {
	if c == nil {
		return nil
	}
	f := &File{c: c}
	runtime.SetFinalizer(f, (*File).Free)
	return f
}

func (f *File) Free() {
	if f.c != nil {
		C.vx_file_free(f.c)
		f.c = nil
		runtime.SetFinalizer(f, nil)
	}
}

func (f *File) Clone() *File {
	return wrapFile(C.vx_file_clone(f.c))
}

type FileOpenOptions struct {
	URI          string
	PropertyKeys []string
	PropertyVals []string
}

func OpenReader(session *Session, opts FileOpenOptions) (*File, error) {
	cURI := C.CString(opts.URI)
	defer C.free(unsafe.Pointer(cURI))

	cKeys, freeKeys := cStringList(opts.PropertyKeys)
	defer freeKeys()
	cVals, freeVals := cStringList(opts.PropertyVals)
	defer freeVals()

	cOpts := C.vx_file_open_options{
		uri:           cURI,
		property_keys: cKeys,
		property_vals: cVals,
		property_len:  C.int(len(opts.PropertyKeys)),
	}

	var cErr *C.vx_error
	c := C.vx_file_open_reader(session.c, &cOpts, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}
	return wrapFile(c), nil
}

func WriteArray(session *Session, path string, array *Array) error {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var cErr *C.vx_error
	C.vx_file_write_array(session.c, cPath, array.c, &cErr)
	return checkError(cErr)
}

func (f *File) RowCount() uint64 {
	return uint64(C.vx_file_row_count(f.c))
}

func (f *File) DType() *DType {
	return wrapDType(C.vx_file_dtype(f.c), false)
}

func (f *File) CanPrune(session *Session, filterExpression string) (bool, error) {
	cExpr := C.CString(filterExpression)
	defer C.free(unsafe.Pointer(cExpr))
	var cErr *C.vx_error
	res := C.vx_file_can_prune(session.c, f.c, cExpr, C.uint(len(filterExpression)), &cErr)
	if err := checkError(cErr); err != nil {
		return false, err
	}
	return bool(res), nil
}

type FileScanOptions struct {
	ProjectionExpression string
	FilterExpression     string
	SplitByRowCount      int
	RowRangeStart        uint64
	RowRangeEnd          uint64
	RowOffset            uint64
}

func (f *File) Scan(session *Session, opts *FileScanOptions) (*ArrayIterator, error) {
	var cOpts *C.vx_file_scan_options
	var cOptsVal C.vx_file_scan_options

	var cProj, cFilt *C.char
	if opts != nil {
		cOpts = &cOptsVal
		if opts.ProjectionExpression != "" {
			cProj = C.CString(opts.ProjectionExpression)
			defer C.free(unsafe.Pointer(cProj))
			cOpts.projection_expression = cProj
			cOpts.projection_expr_len = C.uint(len(opts.ProjectionExpression))
		}
		if opts.FilterExpression != "" {
			cFilt = C.CString(opts.FilterExpression)
			defer C.free(unsafe.Pointer(cFilt))
			cOpts.filter_expression = cFilt
			cOpts.filter_expression_len = C.uint(len(opts.FilterExpression))
		}
		cOpts.split_by_row_count = C.int(opts.SplitByRowCount)
		cOpts.row_range_start = C.ulong(opts.RowRangeStart)
		cOpts.row_range_end = C.ulong(opts.RowRangeEnd)
		cOpts.row_offset = C.ulong(opts.RowOffset)
	}

	var cErr *C.vx_error
	c := C.vx_file_scan(session.c, f.c, cOpts, &cErr)
	if err := checkError(cErr); err != nil {
		return nil, err
	}

	i := &ArrayIterator{c: c}
	runtime.SetFinalizer(i, (*ArrayIterator).Free)
	return i, nil
}
