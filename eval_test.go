package goeval

import (
	"fmt"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	s := NewScope()
	s.Set("print", fmt.Println)
	t.Log(s.Eval(`count := 0`))
	t.Log(s.Eval(`for i:=0;i<100;i=i+1 { 
			count=count+i
		}`))
	t.Log(s.Eval(`print(count)`))
}

func TestNewScope(t *testing.T) {
	s := NewScope()
	//t.Log(s.Eval("\"ab\" + \"cd\""))
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

func BenchmarkScope_Eval(b *testing.B) {

	s := NewScope()
	for i := 0; i < b.N; i++ {

		v, e := s.Eval("\"ab\" + \"cd\"")
		if e != nil {
			panic(e)
		}
		_ = v
	}
}
