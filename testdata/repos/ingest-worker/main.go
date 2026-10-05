// Command ingest-worker moves events from a Redis stream into daily files.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("ingest-worker reading", os.Getenv("REDIS_URL"))
}
