// +build fuzz

package best

import (
	"github.com/coredns/coredns/plugin/pkg/fuzz"
)

// Fuzz fuzzes cache.
func Fuzz(data []byte) int {
	c := Best{}
	return fuzz.Do(c, data)
}
