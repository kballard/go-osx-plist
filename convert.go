package plist

// #import <CoreFoundation/CoreFoundation.h>
// #import <CoreGraphics/CGBase.h> // for CGFloat
import "C"
import "math"
import "reflect"
import "time"
import "unsafe"

func convertValueToCFType(obj interface{}) (C.CFTypeRef, error) {
	value := reflect.ValueOf(obj)
	switch value.Kind() {
	case reflect.Bool:
		return C.CFTypeRef(convertBoolToCFBoolean(value.Bool())), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return C.CFTypeRef(convertInt64ToCFNumber(value.Int())), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return C.CFTypeRef(convertUInt32ToCFNumber(uint32(value.Uint()))), nil
	case reflect.Float32, reflect.Float64:
		return C.CFTypeRef(convertFloat64ToCFNumber(value.Float())), nil
	case reflect.String:
		return C.CFTypeRef(convertStringToCFString(value.String())), nil
	case reflect.Struct:
		// only struct type we support is time.Time
		if value.Type() == reflect.TypeOf(time.Time{}) {
			return C.CFTypeRef(convertTimeToCFDate(obj.(time.Time))), nil
		}
	case reflect.Array, reflect.Slice:
		// check for []byte first (byte is uint8)
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return C.CFTypeRef(convertBytesToCFData(obj.([]byte))), nil
		}
		ary, err := convertSliceToCFArray(value)
		return C.CFTypeRef(ary), err
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			// we can only support maps with a string key
			return nil, &UnsupportedTypeError{value.Type()}
		}
		dict, err := convertMapToCFDictionary(value)
		return C.CFTypeRef(dict), err
	}
	return nil, &UnsupportedTypeError{value.Type()}
}

// we shouldn't ever get an error from this, but I'd rather not panic
func convertCFTypeToValue(cfType C.CFTypeRef) (interface{}, error) {
	typeId := C.CFGetTypeID(cfType)
	switch typeId {
	case C.CFStringGetTypeID():
		return convertCFStringToString(C.CFStringRef(cfType)), nil
	case C.CFNumberGetTypeID():
		return convertCFNumberToInterface(C.CFNumberRef(cfType)), nil
	case C.CFBooleanGetTypeID():
		return convertCFBooleanToBool(C.CFBooleanRef(cfType)), nil
	case C.CFDataGetTypeID():
		return convertCFDataToBytes(C.CFDataRef(cfType)), nil
	case C.CFDateGetTypeID():
		return convertCFDateToTime(C.CFDateRef(cfType)), nil
	case C.CFArrayGetTypeID():
		ary, err := convertCFArrayToSlice(C.CFArrayRef(cfType))
		return ary, err
	case C.CFDictionaryGetTypeID():
		dict, err := convertCFDictionaryToMap(C.CFDictionaryRef(cfType))
		return dict, err
	}
	return nil, &UnknownCFTypeError{int(typeId)}
}

// ===== CFData =====
func convertBytesToCFData(data []byte) C.CFDataRef {
	return C.CFDataCreate(nil, (*C.UInt8)(&data[0]), C.CFIndex(len(data)))
}

func convertCFDataToBytes(cfData C.CFDataRef) []byte {
	bytes := C.CFDataGetBytePtr(cfData)
	return C.GoBytes(unsafe.Pointer(bytes), C.int(C.CFDataGetLength(cfData)))
}

// ===== CFString =====
func convertStringToCFString(str string) C.CFStringRef {
	// go through unsafe to get the string bytes directly without the copy
	header := (*reflect.StringHeader)(unsafe.Pointer(&str))
	bytes := (*C.UInt8)(unsafe.Pointer(header.Data))
	return C.CFStringCreateWithBytes(nil, bytes, C.CFIndex(header.Len), C.kCFStringEncodingUTF8, C.false)
}

func convertCFStringToString(cfStr C.CFStringRef) string {
	cstrPtr := C.CFStringGetCStringPtr(cfStr, C.kCFStringEncodingUTF8)
	if cstrPtr != nil {
		return C.GoString(cstrPtr)
	}
	// quick path doesn't work, so copy the bytes out to a buffer
	length := C.CFStringGetLength(cfStr)
	cfRange := C.CFRange{0, length}
	enc := C.CFStringEncoding(C.kCFStringEncodingUTF8)
	// first find the buffer size necessary
	var usedBufLen C.CFIndex
	if C.CFStringGetBytes(cfStr, cfRange, enc, 0, C.false, nil, 0, &usedBufLen) > 0 {
		bytes := make([]byte, 0, usedBufLen)
		buffer := (*C.UInt8)(unsafe.Pointer(&bytes[0]))
		if C.CFStringGetBytes(cfStr, cfRange, enc, 0, C.false, buffer, usedBufLen, nil) > 0 {
			// bytes is now filled up
			// convert it to a string
			header := (*reflect.SliceHeader)(unsafe.Pointer(&bytes))
			strHeader := reflect.StringHeader{
				Data: header.Data,
				Len:  int(usedBufLen),
			}
			return *(*string)(unsafe.Pointer(&strHeader))
		}
	}

	// we failed to convert, for some reason. Too bad there's no nil string
	return ""
}

// ===== CFDate =====
func convertTimeToCFDate(t time.Time) C.CFDateRef {
	nano := C.double(t.UnixNano()) / C.double(time.Second)
	nano -= C.kCFAbsoluteTimeIntervalSince1970
	return C.CFDateCreate(nil, C.CFAbsoluteTime(nano))
}

func convertCFDateToTime(cfDate C.CFDateRef) time.Time {
	nano := C.double(C.CFDateGetAbsoluteTime(cfDate))
	nano += C.kCFAbsoluteTimeIntervalSince1970
	sec := math.Trunc(float64(nano))
	nsec := (float64(nano) - sec) * float64(time.Second)
	return time.Unix(int64(sec), int64(nsec))
}

// ===== CFBoolean =====
func convertBoolToCFBoolean(b bool) C.CFBooleanRef {
	if b {
		return C.kCFBooleanTrue
	}
	return C.kCFBooleanFalse
}

func convertCFBooleanToBool(cfBoolean C.CFBooleanRef) bool {
	return C.CFBooleanGetValue(cfBoolean) != 0
}

// ===== CFNumber =====
// for simplicity's sake, only include the largest of any given numeric datatype
func convertInt64ToCFNumber(i int64) C.CFNumberRef {
	sint := C.SInt64(i)
	return C.CFNumberCreate(nil, C.kCFNumberSInt64Type, unsafe.Pointer(&sint))
}

func convertCFNumberToInt64(cfNumber C.CFNumberRef) int64 {
	var sint C.SInt64
	C.CFNumberGetValue(cfNumber, C.kCFNumberSInt64Type, unsafe.Pointer(&sint))
	return int64(sint)
}

// there is no uint64 CFNumber type, so we have to use the SInt64 one
func convertUInt32ToCFNumber(u uint32) C.CFNumberRef {
	sint := C.SInt64(u)
	return C.CFNumberCreate(nil, C.kCFNumberSInt64Type, unsafe.Pointer(&sint))
}

func convertCFNumberToUInt32(cfNumber C.CFNumberRef) uint32 {
	var sint C.SInt64
	C.CFNumberGetValue(cfNumber, C.kCFNumberSInt64Type, unsafe.Pointer(&sint))
	return uint32(sint)
}

func convertFloat64ToCFNumber(f float64) C.CFNumberRef {
	double := C.double(f)
	return C.CFNumberCreate(nil, C.kCFNumberDoubleType, unsafe.Pointer(&double))
}

func convertCFNumberToFloat64(cfNumber C.CFNumberRef) float64 {
	var double C.double
	C.CFNumberGetValue(cfNumber, C.kCFNumberDoubleType, unsafe.Pointer(&double))
	return float64(double)
}

// Converts the CFNumberRef to the most appropriate numeric type
func convertCFNumberToInterface(cfNumber C.CFNumberRef) interface{} {
	var value reflect.Value
	var ptr unsafe.Pointer
	typ := C.CFNumberGetType(cfNumber)
	switch typ {
	case C.kCFNumberSInt8Type:
		var sint C.SInt8
		ptr = unsafe.Pointer(&sint)
		value = reflect.ValueOf(int8(0))
	case C.kCFNumberSInt16Type:
		var sint C.SInt16
		ptr = unsafe.Pointer(&sint)
		value = reflect.ValueOf(int16(0))
	case C.kCFNumberSInt32Type:
		var sint C.SInt32
		ptr = unsafe.Pointer(&sint)
		value = reflect.ValueOf(int32(0))
	case C.kCFNumberSInt64Type:
		var sint C.SInt64
		ptr = unsafe.Pointer(&sint)
		value = reflect.ValueOf(int64(0))
	case C.kCFNumberFloat32Type:
		var float C.Float32
		ptr = unsafe.Pointer(&float)
		value = reflect.ValueOf(float32(0))
	case C.kCFNumberFloat64Type:
		var float C.Float64
		ptr = unsafe.Pointer(&float)
		value = reflect.ValueOf(float64(0))
	case C.kCFNumberCharType:
		var char C.char
		ptr = unsafe.Pointer(&char)
		value = reflect.ValueOf(byte(0))
	case C.kCFNumberShortType:
		var short C.short
		ptr = unsafe.Pointer(&short)
		value = reflect.ValueOf(int16(0))
	case C.kCFNumberIntType:
		var i C.int
		ptr = unsafe.Pointer(&i)
		value = reflect.ValueOf(int(0))
	case C.kCFNumberLongType:
		var long C.long
		ptr = unsafe.Pointer(&long)
		value = reflect.ValueOf(int64(0))
	case C.kCFNumberLongLongType:
		// this is the only type that may actually overflow us
		var longlong C.longlong
		ptr = unsafe.Pointer(&longlong)
		value = reflect.ValueOf(int64(0))
	case C.kCFNumberFloatType:
		var float C.float
		ptr = unsafe.Pointer(&float)
		value = reflect.ValueOf(float32(0))
	case C.kCFNumberDoubleType:
		var double C.double
		ptr = unsafe.Pointer(&double)
		value = reflect.ValueOf(float64(0))
	case C.kCFNumberCFIndexType:
		// CFIndex is a long
		var cfIndex C.CFIndex
		ptr = unsafe.Pointer(&cfIndex)
		value = reflect.ValueOf(int64(0))
	case C.kCFNumberNSIntegerType:
		// We don't have a definition of NSInteger, but we know it's either an int or a long
		var nsInt C.long
		ptr = unsafe.Pointer(&nsInt)
		value = reflect.ValueOf(int64(0))
	case C.kCFNumberCGFloatType:
		// CGFloat is a float or double
		var cgFloat C.CGFloat
		ptr = unsafe.Pointer(&cgFloat)
		value = reflect.ValueOf(float64(0))
	}
	C.CFNumberGetValue(cfNumber, typ, ptr)
	return value.Interface()
}

// ===== CFArray =====
// use reflect.Value to support slices of any type
func convertSliceToCFArray(slice reflect.Value) (C.CFArrayRef, error) {
	// assume slice is a slice/array, because our caller already checked
	plists := make([]C.CFTypeRef, slice.Len())
	// defer the release
	defer func() {
		for _, cfObj := range plists {
			if cfObj != nil {
				C.CFRelease(cfObj)
			}
		}
	}()
	// convert the slice
	for i := 0; i < slice.Len(); i++ {
		cfType, err := convertValueToCFType(slice.Index(i))
		if err != nil {
			return nil, err
		}
		plists[i] = cfType
	}

	// create the array
	callbacks := (*C.CFArrayCallBacks)(&C.kCFTypeArrayCallBacks)
	return C.CFArrayCreate(nil, (*unsafe.Pointer)(&plists[0]), C.CFIndex(len(plists)), callbacks), nil
}

func convertCFArrayToSlice(cfArray C.CFArrayRef) ([]interface{}, error) {
	count := C.CFArrayGetCount(cfArray)
	cfTypes := make([]C.CFTypeRef, int(count))
	cfRange := C.CFRange{0, count}
	C.CFArrayGetValues(cfArray, cfRange, (*unsafe.Pointer)(&cfTypes[0]))
	result := make([]interface{}, int(count))
	for i, cfObj := range cfTypes {
		val, err := convertCFTypeToValue(cfObj)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// ===== CFDictionary =====
// use reflect.Value to support maps of any type
func convertMapToCFDictionary(m reflect.Value) (C.CFDictionaryRef, error) {
	// assume m is a map, because our caller already checked
	mapKeys := m.MapKeys()
	keys := make([]C.CFTypeRef, len(mapKeys))
	values := make([]C.CFTypeRef, len(mapKeys))
	// defer the release
	defer func() {
		for _, cfKey := range keys {
			if cfKey != nil {
				C.CFRelease(cfKey)
			}
		}
		for _, cfVal := range values {
			if cfVal != nil {
				C.CFRelease(cfVal)
			}
		}
	}()
	// create the keys and values slices
	for i, keyVal := range mapKeys {
		// keyVal is a Value representing a string
		keys[i] = C.CFTypeRef(convertStringToCFString(keyVal.String()))
		cfObj, err := convertValueToCFType(m.MapIndex(keyVal).Interface())
		if err != nil {
			return nil, err
		}
		values[i] = cfObj
	}
	// create the dictionary
	keyCallbacks := (*C.CFDictionaryKeyCallBacks)(&C.kCFTypeDictionaryKeyCallBacks)
	valCallbacks := (*C.CFDictionaryValueCallBacks)(&C.kCFTypeDictionaryValueCallBacks)
	return C.CFDictionaryCreate(nil, (*unsafe.Pointer)(&keys[0]), (*unsafe.Pointer)(&values[0]), C.CFIndex(len(mapKeys)), keyCallbacks, valCallbacks), nil
}

func convertCFDictionaryToMap(cfDict C.CFDictionaryRef) (map[string]interface{}, error) {
	count := int(C.CFDictionaryGetCount(cfDict))
	cfKeys := make([]C.CFTypeRef, count)
	cfVals := make([]C.CFTypeRef, count)
	C.CFDictionaryGetKeysAndValues(cfDict, (*unsafe.Pointer)(&cfKeys[0]), (*unsafe.Pointer)(&cfVals[0]))
	m := make(map[string]interface{}, count)
	for i := 0; i < count; i++ {
		cfKey := cfKeys[i]
		typeId := C.CFGetTypeID(cfKey)
		if typeId != C.CFStringGetTypeID() {
			return nil, &UnexpectedKeyTypeError{int(typeId)}
		}
		key := convertCFStringToString(C.CFStringRef(cfKey))
		val, err := convertCFTypeToValue(cfVals[i])
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
	return m, nil
}