package goeval

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
)

var (
	Builtins = map[string]interface{}{
		"nil":    nil,
		"true":   true,
		"false":  false,
		"append": Append,
		"make":   Make,
		"len":    Len,
	}
)

// variable scope, recursive definition
type Scope struct {
	Vars   map[string]interface{} // all variables in current scope
	Parent *Scope
}

// create a new variable scope
func NewScope() *Scope {
	s := &Scope{
		Vars: map[string]interface{}{},
	}
	return s
}

// search variable from inner-most scope
func (s *Scope) Get(name string) (val interface{}) {
	currentScope := s
	exists := false
	for !exists && currentScope != nil {
		val, exists = currentScope.Vars[name]
		currentScope = currentScope.Parent
	}
	return
}

func (s *Scope) GetJsonString(name string) (val string) {
	b, err := json.Marshal(s.Get(name))
	if err != nil {
		return "null"
	}
	return string(b)
}

// Set walks the scope and sets a value in a parent scope if it exists, else current.
func (s *Scope) Set(name string, val interface{}) {
	exists := false
	currentScope := s
	for !exists && currentScope != nil {
		_, exists = currentScope.Vars[name]
		if exists {
			currentScope.Vars[name] = val
		}
		currentScope = currentScope.Parent
	}
	if !exists {
		s.Vars[name] = val
	}
}

// Keys returns all keys in scope
func (s *Scope) Keys() (keys []string) {
	currentScope := s
	for currentScope != nil {
		for k := range currentScope.Vars {
			keys = append(keys, k)
		}
		currentScope = s.Parent
	}
	return
}

// NewChild creates a scope under the existing scope.
func (s *Scope) NewChild() *Scope {
	child := NewScope()
	child.Parent = s
	return child
}

// Eval evaluates a string
func (s *Scope) Eval(str string) (interface{}, error) {
	expr, err := parser.ParseExpr("func(){" + str + "}()")
	if err != nil {
		return nil, err
	}
	return s.Interpret(expr.(*ast.CallExpr).Fun.(*ast.FuncLit).Body)
}

// Interpret interprets an ast.Node and returns the value.
func (s *Scope) Interpret(expr ast.Node) (interface{}, error) {
	switch e := expr.(type) {
	case *ast.Ident: // An Ident node represents an identifier.
		typ, err := StringToType(e.Name)
		if err == nil {
			return typ, err
		}

		if obj, exists := Builtins[e.Name]; exists {
			return obj, nil
		}

		if obj := s.Get(e.Name); obj != nil {
			return obj, nil
		}
		return nil, fmt.Errorf("can't find EXPR %s", e.Name)

	case *ast.SelectorExpr: // A SelectorExpr node represents an expression followed by a selector.
		X, err := s.Interpret(e.X)
		if err != nil {
			return nil, err
		}
		sel := e.Sel

		rVal := reflect.ValueOf(X)
		if rVal.Kind() != reflect.Struct && rVal.Kind() != reflect.Ptr {
			return nil, fmt.Errorf("%#v is not a struct and thus has no field %#v", X, sel.Name)
		}

		if method := rVal.MethodByName(sel.Name); method.IsValid() {
			return method.Interface(), nil
		}
		if rVal.Kind() == reflect.Ptr {
			rVal = rVal.Elem()
		}
		if field := rVal.FieldByName(sel.Name); field.IsValid() {
			return field.Interface(), nil
		}
		return nil, fmt.Errorf("unknown field %#v", sel.Name)

	case *ast.CallExpr:
		fun, err := s.Interpret(e.Fun)
		if err != nil {
			return nil, err
		}

		// make sure fun is a function
		rf := reflect.ValueOf(fun)
		if rf.Kind() != reflect.Func {
			return nil, fmt.Errorf("not a function %#v", fun)
		}

		// interpret args
		args := make([]reflect.Value, len(e.Args))
		for i, arg := range e.Args {
			interpretedArg, err := s.Interpret(arg)
			if err != nil {
				return nil, err
			}
			args[i] = reflect.ValueOf(interpretedArg)
		}

		// call
		values := ValuesToInterfaces(rf.Call(args))
		if len(values) == 0 {
			return nil, nil
		} else if len(values) == 1 {
			return values[0], nil
		}
		err, _ = values[1].(error)
		return values[0], err

	case *ast.BasicLit:
		switch e.Kind {
		case token.INT:
			n, err := strconv.ParseInt(e.Value, 0, 64)
			return int(n), err
		case token.FLOAT, token.IMAG:
			return strconv.ParseFloat(e.Value, 64)
		case token.CHAR:
			return (rune)(e.Value[1]), nil
		case token.STRING:
			return e.Value[1 : len(e.Value)-1], nil
		default:
			return nil, fmt.Errorf("unknown basic literal %d", e.Kind)
		}

	case *ast.CompositeLit:
		typ, err := s.Interpret(e.Type)
		if err != nil {
			return nil, err
		}

		switch t := e.Type.(type) {
		case *ast.ArrayType:
			l := len(e.Elts)
			slice := reflect.MakeSlice(typ.(reflect.Type), l, l)
			for i, elem := range e.Elts {
				elemValue, err := s.Interpret(elem)
				if err != nil {
					return nil, err
				}
				slice.Index(i).Set(reflect.ValueOf(elemValue))
			}
			return slice.Interface(), nil

		case *ast.MapType:
			nMap := reflect.MakeMap(typ.(reflect.Type))
			for _, elem := range e.Elts {
				switch eT := elem.(type) {
				case *ast.KeyValueExpr:
					key, err := s.Interpret(eT.Key)
					if err != nil {
						return nil, err
					}
					val, err := s.Interpret(eT.Value)
					if err != nil {
						return nil, err
					}
					nMap.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(val))

				default:
					return nil, fmt.Errorf("invalid element type %#v to map. Expecting key value pair", eT)
				}
			}
			return nMap.Interface(), nil

		default:
			return nil, fmt.Errorf("unknown composite literal %#v", t)
		}

	case *ast.BinaryExpr:
		x, err := s.Interpret(e.X)
		if err != nil {
			return nil, err
		}
		y, err := s.Interpret(e.Y)
		if err != nil {
			return nil, err
		}
		return ComputeBinaryOp(x, y, e.Op)

	case *ast.UnaryExpr:
		x, err := s.Interpret(e.X)
		if err != nil {
			return nil, err
		}
		return ComputeUnaryOp(x, e.Op)

	case *ast.ArrayType:
		typ, err := s.Interpret(e.Elt)
		if err != nil {
			return nil, err
		}
		arrType := reflect.SliceOf(typ.(reflect.Type))
		return arrType, nil

	case *ast.MapType:
		keyType, err := s.Interpret(e.Key)
		if err != nil {
			return nil, err
		}
		valType, err := s.Interpret(e.Value)
		if err != nil {
			return nil, err
		}
		mapType := reflect.MapOf(keyType.(reflect.Type), valType.(reflect.Type))
		return mapType, nil

	case *ast.ChanType:
		typeI, err := s.Interpret(e.Value)
		if err != nil {
			return nil, err
		}
		typ, isType := typeI.(reflect.Type)
		if !isType {
			return nil, fmt.Errorf("chan needs to be passed a type not %T", typ)
		}
		return reflect.ChanOf(reflect.BothDir, typ), nil

	case *ast.IndexExpr:
		X, err := s.Interpret(e.X)
		if err != nil {
			return nil, err
		}
		i, err := s.Interpret(e.Index)
		if err != nil {
			return nil, err
		}
		xVal := reflect.ValueOf(X)
		if reflect.TypeOf(X).Kind() == reflect.Map {
			val := xVal.MapIndex(reflect.ValueOf(i))
			if !val.IsValid() {
				// If not valid key, return the "zero" type. Eg for int 0, string ""
				return reflect.Zero(xVal.Type().Elem()).Interface(), nil
			}
			return val.Interface(), nil
		}

		iVal, isInt := i.(int)
		if !isInt {
			return nil, fmt.Errorf("index has to be an int not %T", i)
		}
		if iVal >= xVal.Len() || iVal < 0 {
			return nil, errors.New("slice index out of range")
		}
		return xVal.Index(iVal).Interface(), nil

	case *ast.SliceExpr:
		low, err := s.Interpret(e.Low)
		if err != nil {
			return nil, err
		}
		high, err := s.Interpret(e.High)
		if err != nil {
			return nil, err
		}
		X, err := s.Interpret(e.X)
		if err != nil {
			return nil, err
		}
		xVal := reflect.ValueOf(X)
		if low == nil {
			low = 0
		}
		if high == nil {
			high = xVal.Len()
		}
		lowVal, isLowInt := low.(int)
		highVal, isHighInt := high.(int)
		if !isLowInt || !isHighInt {
			return nil, fmt.Errorf("slice: indexes have to be an ints not %T and %T", low, high)
		}
		if lowVal < 0 || highVal >= xVal.Len() {
			return nil, errors.New("slice: index out of bounds")
		}
		return xVal.Slice(lowVal, highVal).Interface(), nil

	case *ast.ParenExpr:
		return s.Interpret(e.X)

	case *ast.ReturnStmt:
		results := make([]interface{}, len(e.Results))
		for i, result := range e.Results {
			out, err := s.Interpret(result)
			if err != nil {
				return out, err
			}
			results[i] = out
		}

		if len(results) == 0 {
			return nil, nil
		} else if len(results) == 1 {
			return results[0], nil
		}
		return results, nil

	case *ast.AssignStmt:
		define := e.Tok == token.DEFINE
		lhs := make([]string, len(e.Lhs))
		for i, id := range e.Lhs {
			lhsIdent, isIdent := id.(*ast.Ident)
			if !isIdent {
				return nil, fmt.Errorf("%#v assignment is not ident", id)
			}
			lhs[i] = lhsIdent.Name
		}
		rhs := make([]interface{}, len(e.Rhs))
		for i, expr := range e.Rhs {
			val, err := s.Interpret(expr)
			if err != nil {
				return nil, err
			}
			rhs[i] = val
		}

		if len(rhs) != 1 && len(rhs) != len(lhs) {
			return nil, fmt.Errorf("assignment count mismatch: %d = %d", len(lhs), len(rhs))
		}
		if len(rhs) == 1 && len(lhs) > 1 && reflect.TypeOf(rhs[0]).Kind() == reflect.Slice {
			rhsV := reflect.ValueOf(rhs[0])
			rhsLen := rhsV.Len()
			if rhsLen != len(lhs) {
				return nil, fmt.Errorf("assignment count mismatch: %d = %d", len(lhs), rhsLen)
			}
			for i := 0; i < rhsLen; i++ {
				variable := lhs[i]
				v := s.Get(variable)
				if v == nil && !define {
					return nil, fmt.Errorf("variable %#v is not defined", variable)
				}
				s.Set(variable, rhsV.Index(i).Interface())
			}
		} else {
			for i, r := range rhs {
				variable := lhs[i]
				v := s.Get(variable)
				if v == nil && !define {
					return nil, fmt.Errorf("variable %#v is not defined", variable)
				}
				s.Set(variable, r)
			}
		}

		if len(rhs) > 1 {
			return rhs, nil
		}
		return rhs[0], nil

	case *ast.ForStmt:
		s := s.NewChild()
		s.Interpret(e.Init)
		for {
			ok, err := s.Interpret(e.Cond)
			if err != nil {
				return nil, err
			}
			if !ok.(bool) {
				break
			}
			s.Interpret(e.Body)
			s.Interpret(e.Post)
		}
		return nil, nil
	case *ast.RangeStmt:
		s := s.NewChild()
		ranger, err := s.Interpret(e.X)
		if err != nil {
			return nil, err
		}
		var key, value string
		if e.Key != nil {
			key = e.Key.(*ast.Ident).Name
		}
		if e.Value != nil {
			value = e.Value.(*ast.Ident).Name
		}
		rv := reflect.ValueOf(ranger)
		switch rv.Type().Kind() {
		case reflect.Array, reflect.Slice:
			for i := 0; i < rv.Len(); i++ {
				if len(key) > 0 {
					s.Set(key, i)
				}
				if len(value) > 0 {
					s.Set(value, rv.Index(i).Interface())
				}
				s.Interpret(e.Body)
			}
		case reflect.Map:
			keys := rv.MapKeys()
			for _, keyV := range keys {
				if len(key) > 0 {
					s.Set(key, keyV.Interface())
				}
				if len(value) > 0 {
					s.Set(value, rv.MapIndex(keyV).Interface())
				}
				s.Interpret(e.Body)
			}
		default:
			return nil, fmt.Errorf("ranging on %s is unsupported", rv.Type().Kind().String())
		}
		return nil, nil
	case *ast.ExprStmt:
		return s.Interpret(e.X)
	case *ast.DeclStmt:
		return s.Interpret(e.Decl)
	case *ast.GenDecl:
		for _, spec := range e.Specs {
			if _, err := s.Interpret(spec); err != nil {
				return nil, err
			}
		}
		return nil, nil
	case *ast.ValueSpec:
		typ, err := s.Interpret(e.Type)
		if err != nil {
			return nil, err
		}
		zero := reflect.Zero(typ.(reflect.Type)).Interface()
		for i, name := range e.Names {
			if len(e.Values) > i {
				v, err := s.Interpret(e.Values[i])
				if err != nil {
					return nil, err
				}
				s.Set(name.Name, v)
			} else {
				s.Set(name.Name, zero)
			}
		}
		return nil, nil
	case *ast.BlockStmt:
		var outFinal interface{}
		for _, stmts := range e.List {
			out, err := s.Interpret(stmts)
			if err != nil {
				return out, err
			}
			outFinal = out
		}
		return outFinal, nil

	default:
		return nil, fmt.Errorf("unknown node %#v", e)
	}
}

// StringToType returns the reflect.Type corresponding to the type string provided. Ex: StringToType("int")
func StringToType(str string) (reflect.Type, error) {
	builtinTypes := map[string]reflect.Type{
		"bool":       reflect.TypeOf(true),
		"byte":       reflect.TypeOf(byte(0)),
		"rune":       reflect.TypeOf(rune(0)),
		"string":     reflect.TypeOf(""),
		"int":        reflect.TypeOf(int(0)),
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
	val, present := builtinTypes[str]
	if !present {
		return nil, fmt.Errorf("type %#v is not in table", str)
	}
	return val, nil
}

// ValuesToInterfaces converts a slice of []reflect.Value to []interface{}
func ValuesToInterfaces(values []reflect.Value) []interface{} {
	inters := make([]interface{}, len(values))
	for i, val := range values {
		inters[i] = val.Interface()
	}
	return inters
}
