// Command packages prints every Ubuntu package name in Doctor's knowledge, one
// per line, for tools/doctor/check-packages.sh.
package main

import (
	"fmt"

	"preconfiguration.com/preconfig/internal/doctor"
)

func main() {
	for _, p := range doctor.AllPackages() {
		fmt.Println(p)
	}
}
