package tbot

import (
	"fmt"
	"strconv"
	"testing"
)


func BenchmarkItoa(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = strconv.Itoa(123456)
	}

}

func BenchmarkSprintf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%d", 123456)
	}
}