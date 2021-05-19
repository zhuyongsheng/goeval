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

func astPrint(src string) {
	fSet := token.NewFileSet() // positions are relative to fset
	f, err := parser.ParseFile(fSet, "", `package main
	func main() {`+src+`}`, 0)
	if err != nil {
		panic(err)
	}
	// Print the AST.
	_ = ast.Print(fSet, f)
}

// Eval evaluates a string
func (s *Scope) Eval(src string) (interface{}, error) {
	expr, err := parser.ParseExpr("func(){" + src + "}()")
	if err != nil {
		return nil, err
	}
	//astPrint(src)
	return s.interpret(expr.(*ast.CallExpr).Fun.(*ast.FuncLit).Body)
}

func (s *Scope) interpret(body ast.Node) (interface{}, error) {
	switch node := body.(type) {
	case ast.Decl:
		switch decl := node.(type) {
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				if _, err := s.interpret(spec); err != nil {
					return nil, err
				}
			}
			return nil, nil
		default:
			return nil, fmt.Errorf("goeval: unknown DECL %#v", decl)
		}
	case ast.Expr:
		switch expr := node.(type) {
		case *ast.ArrayType:
			typ, err := s.interpret(expr.Elt)
			if err != nil {
				return nil, err
			}
			arrType := reflect.SliceOf(typ.(reflect.Type))
			return arrType, nil
		case *ast.BasicLit:
			switch expr.Kind {
			case token.INT:
				n, err := strconv.ParseInt(expr.Value, 0, 64)
				return int(n), err
			case token.FLOAT, token.IMAG:
				return strconv.ParseFloat(expr.Value, 64)
			case token.CHAR:
				return (rune)(expr.Value[1]), nil
			case token.STRING:
				return expr.Value[1 : len(expr.Value)-1], nil
			default:
				return nil, fmt.Errorf("goeval: unknown BasicLit %#v", expr)
			}
		case *ast.BinaryExpr:
			x, err := s.interpret(expr.X)
			if err != nil {
				return nil, err
			}
			y, err := s.interpret(expr.Y)
			if err != nil {
				return nil, err
			}
			return binaryOp(x, y, expr.Op)
		case *ast.CallExpr:
			fun, err := s.interpret(expr.Fun)
			if err != nil {
				return nil, err
			}
			rf := reflect.ValueOf(fun)
			// make sure fun is a function
			if rf.Kind() != reflect.Func {
				return nil, fmt.Errorf("goeval: %#v not a function", fun)
			}
			// interpret args
			args := make([]reflect.Value, len(expr.Args))
			for i, arg := range expr.Args {
				av, err := s.interpret(arg)
				if err != nil {
					return nil, err
				}
				args[i] = reflect.ValueOf(av)
			}
			// call
			values := interfaced(rf.Call(args))
			if len(values) == 0 {
				return nil, nil
			}
			if len(values) == 1 {
				return values[0], nil
			}
			err, _ = values[1].(error)
			return values[0], err
		case *ast.ChanType:
			typeI, err := s.interpret(expr.Value)
			if err != nil {
				return nil, err
			}
			typ, isType := typeI.(reflect.Type)
			if !isType {
				return nil, fmt.Errorf("goeval: %#v not a type for chan", typ)
			}
			return reflect.ChanOf(reflect.BothDir, typ), nil
		case *ast.CompositeLit:
			typ, err := s.interpret(expr.Type)
			if err != nil {
				return nil, err
			}
			switch t := expr.Type.(type) {
			case *ast.ArrayType:
				l := len(expr.Elts)
				slice := reflect.MakeSlice(typ.(reflect.Type), l, l)
				for i, elt := range expr.Elts {
					elemValue, err := s.interpret(elt)
					if err != nil {
						return nil, err
					}
					slice.Index(i).Set(reflect.ValueOf(elemValue))
				}
				return slice.Interface(), nil
			case *ast.MapType:
				nMap := reflect.MakeMap(typ.(reflect.Type))
				for _, elt := range expr.Elts {
					switch eT := elt.(type) {
					case *ast.KeyValueExpr:
						key, err := s.interpret(eT.Key)
						if err != nil {
							return nil, err
						}
						val, err := s.interpret(eT.Value)
						if err != nil {
							return nil, err
						}
						nMap.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(val))
					default:
						return nil, fmt.Errorf("goeval: invalid element type %#v to map", eT)
					}
				}
				return nMap.Interface(), nil
			case *ast.StructType:
				nStruct := reflect.New(typ.(reflect.Type)).Interface()
				rv := reflect.ValueOf(nStruct).Elem()
				for _, elt := range expr.Elts {
					switch eT := elt.(type) {
					case *ast.KeyValueExpr:
						key, err := s.interpret(eT.Key)
						if err != nil {
							return nil, err
						}
						val, err := s.interpret(eT.Value)
						if err != nil {
							return nil, err
						}
						rv.FieldByName(key.(string)).Set(reflect.ValueOf(val))
					default:
						return nStruct, fmt.Errorf("goeval: unknown element %#v", elt)
					}
				}
				return nStruct, nil
			case *ast.Ident:
				nStruct := reflect.New(typ.(reflect.Type))
				rv := reflect.ValueOf(nStruct.Interface()).Elem()
				for _, elt := range expr.Elts {
					switch eT := elt.(type) {
					case *ast.KeyValueExpr:
						key, err := s.interpret(eT.Key)
						if err != nil {
							return nil, err
						}
						val, err := s.interpret(eT.Value)
						if err != nil {
							return nil, err
						}
						rv.FieldByName(key.(string)).Set(reflect.ValueOf(val))
					default:
						return nStruct.Elem(), fmt.Errorf("goeval: unknown element %#v", elt)
					}
				}
				return nStruct.Elem(), nil
			default:
				return nil, fmt.Errorf("goeval: unknown composite literal %#v", t)
			}
		case *ast.Ident: // An Ident node represents an identifier.
			if expr.Obj == nil {
				return expr.Name, nil
			}
			switch expr.Obj.Kind {
			case ast.Bad:
				if v, ok := builtinTypes[expr.Name]; ok {
					return v, nil
				}
				if v, ok := builtins[expr.Name]; ok {
					return v, nil
				}
				if v := s.Get(expr.Name); v != nil {
					return v, nil
				}
				return expr.Name, nil
			case ast.Typ:
				if typ, ok := s.Vars[expr.Name]; ok {
					return typ, nil
				} else {
					return nil, fmt.Errorf("goeval: type %s not found", expr.Name)
				}
			case ast.Var:
				if v := s.Get(expr.Name); v != nil {
					return v, nil
				}
			}
		case *ast.IndexExpr:
			X, err := s.interpret(expr.X)
			if err != nil {
				return nil, err
			}
			i, err := s.interpret(expr.Index)
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
				return nil, fmt.Errorf("goeval: index must be an int not %T", i)
			}
			if iVal >= xVal.Len() || iVal < 0 {
				return nil, errors.New("slice index result of range")
			}
			return xVal.Index(iVal).Interface(), nil
		case *ast.MapType:
			keyType, err := s.interpret(expr.Key)
			if err != nil {
				return nil, err
			}
			valType, err := s.interpret(expr.Value)
			if err != nil {
				return nil, err
			}
			mapType := reflect.MapOf(keyType.(reflect.Type), valType.(reflect.Type))
			return mapType, nil
		case *ast.ParenExpr:
			return s.interpret(expr.X)
		case *ast.SelectorExpr:
			x, err := s.interpret(expr.X)
			if err != nil {
				return nil, err
			}
			sel := expr.Sel
			rVal := reflect.ValueOf(x)
			if rVal.Kind() != reflect.Struct && rVal.Kind() != reflect.Ptr {
				return nil, fmt.Errorf("goeval: %#v is not a struct or has no field %#v", x, sel.Name)
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
			return nil, fmt.Errorf("goeval: unknown field %#v", sel.Name)
		case *ast.SliceExpr:
			low, err := s.interpret(expr.Low)
			if err != nil {
				return nil, err
			}
			high, err := s.interpret(expr.High)
			if err != nil {
				return nil, err
			}
			x, err := s.interpret(expr.X)
			if err != nil {
				return nil, err
			}
			xVal := reflect.ValueOf(x)
			if low == nil {
				low = 0
			}
			if high == nil {
				high = xVal.Len()
			}
			lowVal, isLowInt := low.(int)
			highVal, isHighInt := high.(int)
			if !isLowInt || !isHighInt {
				return nil, fmt.Errorf("goeval: slice indexe must be an ints not %T and %T", low, high)
			}
			if lowVal < 0 || highVal >= xVal.Len() {
				return nil, errors.New("slice: index result of bounds")
			}
			return xVal.Slice(lowVal, highVal).Interface(), nil
		case *ast.StructType:
			structFields := make([]reflect.StructField, len(expr.Fields.List))
			for i, field := range expr.Fields.List {
				typ, err := s.interpret(field.Type)
				if err != nil {
					return nil, err
				}
				structFields[i] = reflect.StructField{
					Name:      field.Names[0].Name,
					Type:      typ.(reflect.Type),
					Anonymous: false,
				}
			}
			return reflect.StructOf(structFields), nil
		case *ast.UnaryExpr:
			x, err := s.interpret(expr.X)
			if err != nil {
				return nil, err
			}
			return unaryOp(x, expr.Op)
		default:
			return nil, fmt.Errorf("goeval: unknown EXPR %#v", expr)
		}
	case ast.Spec:
		switch spec := node.(type) {
		case *ast.TypeSpec:
			typ, err := s.interpret(spec.Type)
			if err != nil {
				return nil, err
			}
			s.Vars[spec.Name.Name] = typ.(reflect.Type)
			return typ.(reflect.Type), nil
		case *ast.ValueSpec:
			typ, err := s.interpret(spec.Type)
			if err != nil {
				return nil, err
			}
			zero := reflect.Zero(typ.(reflect.Type)).Interface()
			for i, name := range spec.Names {
				if len(spec.Values) > i {
					v, err := s.interpret(spec.Values[i])
					if err != nil {
						return nil, err
					}
					s.Set(name.Name, v)
				} else {
					s.Set(name.Name, zero)
				}
			}
			return nil, nil
		default:
			return nil, fmt.Errorf("goeval: unknown SPEC %#v", spec)
		}
	case ast.Stmt:
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			if len(stmt.Lhs) != len(stmt.Rhs) {
				return nil, fmt.Errorf("goeval: assignment mismatch: %d != %d", len(stmt.Lhs), len(stmt.Rhs))
			}
			for i, lh := range stmt.Lhs {
				rh, err := s.interpret(stmt.Rhs[i])
				if err != nil {
					return nil, err
				}
				switch variable := lh.(type) {
				case *ast.Ident:
					varName := variable.Name
					v := s.Get(varName)
					if v == nil && (stmt.Tok != token.DEFINE) {
						return nil, fmt.Errorf("goeval: variable %#v not defined", variable)
					}
					if token.ADD_ASSIGN <= stmt.Tok && stmt.Tok <= token.AND_NOT_ASSIGN {
						rh, err = binaryOp(v, rh, stmt.Tok+(token.ADD-token.ADD_ASSIGN))
						if err != nil {
							return nil, err
						}
					}
					s.Set(varName, rh)
				case *ast.IndexExpr:
					x, err := s.interpret(variable.X)
					xVal := reflect.ValueOf(x)
					index, err := s.interpret(variable.Index)
					if err != nil {
						return nil, err
					}
					rhV := reflect.ValueOf(rh)
					switch reflect.TypeOf(x).Kind() {
					case reflect.Map:
						xVal.SetMapIndex(reflect.ValueOf(index), rhV)
					case reflect.Slice:
						xVal.Index(index.(int)).Set(rhV)
					default:
						return nil, fmt.Errorf("goeval: unknown type %v", reflect.TypeOf(x).Kind())
					}
				default:
					return nil, fmt.Errorf("goeval: unknown assignment type %#v", variable)

				}
			}
			return nil, nil
		case *ast.BlockStmt:
			for i, st := range stmt.List {
				result, err := s.interpret(st)
				if err != nil || i == len(stmt.List)-1 {
					return result, err
				}
			}
		case *ast.DeclStmt:
			return s.interpret(stmt.Decl)
		case *ast.ExprStmt:
			return s.interpret(stmt.X)
		case *ast.ForStmt:
			_, err := s.interpret(stmt.Init)
			if err != nil {
				return nil, err
			}
			for {
				ok, err := s.interpret(stmt.Cond)
				if err != nil {
					return nil, err
				}
				if !ok.(bool) {
					break
				}
				_, _ = s.interpret(stmt.Body)
				_, _ = s.interpret(stmt.Post)
			}
			return nil, nil
		case *ast.IfStmt:
			_, _ = s.interpret(stmt.Init)
			cond, err := s.interpret(stmt.Cond)
			if err != nil {
				return nil, err
			}
			if cond.(bool) {
				return s.interpret(stmt.Body)
			}
			if stmt.Else != nil {
				return s.interpret(stmt.Else)
			}
		case *ast.RangeStmt:
			ranger, err := s.interpret(stmt.X)
			if err != nil {
				return nil, err
			}
			var key, value string
			if stmt.Key != nil {
				key = stmt.Key.(*ast.Ident).Name
			}
			if stmt.Value != nil {
				value = stmt.Value.(*ast.Ident).Name
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
					_, _ = s.interpret(stmt.Body)
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
					_, _ = s.interpret(stmt.Body)
				}
			default:
				return nil, fmt.Errorf("goeval: range unsupported on %s", rv.Type().Kind().String())
			}
			return nil, nil
		case *ast.ReturnStmt:
			results := make([]interface{}, len(stmt.Results))
			for i, result := range stmt.Results {
				out, err := s.interpret(result)
				if err != nil {
					return out, err
				}
				results[i] = out
			}

			if len(results) == 0 {
				return nil, nil
			}
			if len(results) == 1 {
				return results[0], nil
			}
			return results, nil
		default:
			return nil, fmt.Errorf("goeval: unknown STMT %#v", stmt)
		}
	default:
		return nil, fmt.Errorf("goeval: unknown NODE %#v", node)
	}
	return nil, nil
}

// interfaced converts a slice of []reflect.Value to []interface{}
func interfaced(values []reflect.Value) []interface{} {
	iValues := make([]interface{}, len(values))
	for i, val := range values {
		iValues[i] = val.Interface()
	}
	return iValues
}
