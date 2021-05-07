package goeval

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	builtins = map[string]interface{}{
		"nil":    nil,
		"true":   true,
		"false":  false,
		"append": Append,
		"make":   Make,
		"len":    Len,
	}
	builtinTypes = map[string]reflect.Type{
		"bool":       reflect.TypeOf(true),
		"byte":       reflect.TypeOf(byte(0)),
		"rune":       reflect.TypeOf(rune(0)),
		"string":     reflect.TypeOf(string(0)),
		"int":        reflect.TypeOf(0),
		"int8":       reflect.TypeOf(int8(0)),
		"int16":      reflect.TypeOf(int16(0)),
		"int32":      reflect.TypeOf(int32(0)),
		"int64":      reflect.TypeOf(int64(0)),
		"uint":       reflect.TypeOf(uint(0)),
		"uint8":      reflect.TypeOf(uint8(0)),
		"uint16":     reflect.TypeOf(uint16(0)),
		"uint32":     reflect.TypeOf(uint32(0)),
		"uint64":     reflect.TypeOf(uint64(0)),
		"uintptr":    reflect.TypeOf(uintptr(0)),
		"float32":    reflect.TypeOf(float32(0)),
		"float64":    reflect.TypeOf(float64(0)),
		"complex64":  reflect.TypeOf(complex64(0)),
		"complex128": reflect.TypeOf(complex128(0)),
		"error":      reflect.TypeOf(errors.New("")),
	}
)


// Append is a runtime replacement for the append function
func Append(arr interface{}, elements ...interface{}) (interface{}, error) {
	arrVal := reflect.ValueOf(arr)
	valArr := make([]reflect.Value, len(elements))
	for i, elem := range elements {
		if reflect.TypeOf(arr) != reflect.SliceOf(reflect.TypeOf(elem)) {
			return nil, fmt.Errorf("%T cannot append to %T", elem, arr)
		}
		valArr[i] = reflect.ValueOf(elem)
	}
	return reflect.Append(arrVal, valArr...).Interface(), nil
}

// Make is a runtime replacement for the make function
func Make(t interface{}, args ...interface{}) (v interface{}, err error) {
	typ, isType := t.(reflect.Type)
	if !isType {
		return nil, fmt.Errorf("invalid type %#v", t)
	}
	switch typ.Kind() {
	case reflect.Slice:
		if len(args) < 1 || len(args) > 2 {
			return nil, errors.New("invalid number of arguments for make slice, 1 or 2 needed")
		}
		length, err := getInteger(args[0])
		if err != nil {
			return nil, err
		}
		capacity := length
		if len(args) == 2 {
			capacity, err = getInteger(args[1])
			if err != nil {
				return nil, err
			}
		}
		return reflect.MakeSlice(typ, length, capacity).Interface(), nil

	case reflect.Chan:
		if len(args) > 1 {
			return nil, errors.New("too many arguments")
		}
		size := 0
		if len(args) == 1 {
			size, err = getInteger(args[0])
			if err != nil {
				return nil, err
			}
		}
		return reflect.MakeChan(typ, size).Interface(), nil
	case reflect.Map:
		size := 0
		if len(args) > 0 {
			size, err = getInteger(args[0])
			if err != nil {
				return nil, err
			}
		}
		return reflect.MakeMapWithSize(typ, size).Interface(), nil
	default:
		return nil, fmt.Errorf("make unsupported type %T", t)
	}
}

// Len is a runtime replacement for the len function
func Len(t interface{}) (interface{}, error) {
	return reflect.ValueOf(t).Len(), nil
}

func getInteger(arg interface{}) (int, error) {
	if i, ok := arg.(int); ok {
		return i, nil
	}
	return 0, errors.New("error not int")
}
