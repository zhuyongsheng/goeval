package goeval

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func Add(a, b int) int {
	return a + b
}

func Current() int64 {
	return time.Now().Unix()
}

func TestPresetFunc(t *testing.T) {
	s := NewScope()
	s.Set("add", Add)
	s.Set("current", Current)
	c := s.NewChild()
	d := s.NewChild()
	c.Set("age", 3)
	t.Log(c.Eval(`add(1,age)`))
	t.Log(c.GetJsonString("age"))
	t.Log(d.GetJsonString("age"))
	t.Log(d.Eval("current()"))
}

func BenchmarkEval(b *testing.B) {

	s := NewScope()
	s.Set("current", Current)

	for i := 0; i < b.N; i++ {
		s.Eval("current()")
	}
}

func BenchmarkEvalCompare(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Current()
	}
}

func TestFor(t *testing.T) {
	s := NewScope()
	s.Set("print", fmt.Println)
	t.Log(s.Eval(`count := 0
		for i:=0;i<100;i=i+1 {
			count=count+i
		}
	print(count)`))
}

func TestIF(t *testing.T) {
	s := NewScope()
	s.Set("print", fmt.Println)
	t.Log(s.Eval(`a := 3
	if a > 0 {
		return "positive"
	} else {
		return "negative"
	}`))
}

func TestEStruct(t *testing.T) {
	s := NewScope()
	s.Set("print", fmt.Println)
	t.Log(s.Eval(`cat := struct {
		Name string
		Age int
	}{
		Name: "tom",
		Age: 1,
	}
	print(cat.Name)`))
	fmt.Printf("%#v", s.Get("cat"))

}

func TestDStruct(t *testing.T) {

	s := NewScope()
	s.Set("print", fmt.Printf)
	t.Log(s.Eval(`type  Animal struct{
		Name string
		Age int
	}
	cat := &Animal{
		Name: "Tom",
		Age:  3,
	}
	print("%#v", cat)`))
}

func TestMakeMap(t *testing.T) {

	s := NewScope()
	s.Set("print", fmt.Println)
	t.Log(s.Eval(`a := make(map[string]int)
	a["tom"] = 3
	a["jerry"] = 5
	print(a)`))
	println(s.GetJsonString("a"))
}

// todo: try to handle import
func TestImport(t *testing.T) {

	s := NewScope()
	//s.Set("ToUpper", strings.ToUpper)
	t.Log(s.Eval(`import "strings"
	a := strings.ToUpper("abc")`))
	println(s.GetJsonString("a"))

}

func TestConcurrent(t *testing.T) {
	s := NewScope()
	for i := 0; i < 100; i++ {
		go func(n int) {
			v, e := s.Eval(fmt.Sprintf(`2 + %d`, n))
			if e != nil {
				panic(e)
			}
			fmt.Println(v)
		}(i)
	}
	time.Sleep(1 * time.Second)
}

func TestScopePreset(t *testing.T) {

	s := NewScope()
	s.Set(`ef`, map[string]int{"xx": 3})
	s.Set(`mn`, []string{"xx", "yy", "zz"})
	s.Set(`bb`, true)
	t.Log(s.GetJsonString(`ef`))
	t.Log(s.GetJsonString(`mn`))
	t.Log(s.GetJsonString(`mx`))
	t.Log(s.GetJsonString(`bb`))
	t.Log(s.Eval("mn[1] + a"))
}

func BenchmarkEvalStringContact(b *testing.B) {
	s := NewScope()
	for i := 0; i < b.N; i++ {
		v, e := s.Eval("\"ab\" + \"cd\"")
		if e != nil {
			panic(e)
		}
		_ = v
	}
}

func TestStringToType(t *testing.T) {
	fmt.Printf("%v\n", reflect.TypeOf(""))
	println(reflect.TypeOf("") == reflect.TypeOf(string(0)))
	var a interface{}
	a = map[string]int{}
	fmt.Printf("%v", reflect.TypeOf(a).Kind())
}

func TestAppend(t *testing.T) {
	s := NewScope()
	t.Log(s.Eval(`a := []int{1,2,3}
	a = append(a, 6)
	b := []int{4,5}
	a = append(a, b...)`))
	fmt.Println(s.GetJsonString("a"))
}
