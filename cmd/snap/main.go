package main

import (
	"fmt"
	"os"

	msdriver "onvif-tool/internal/milesight"
)

func main() {
	cam := msdriver.New("162.191.235.243", "admin", "J3tstr3am0!")
	snap, err := cam.Snapshot()
	if err != nil {
		fmt.Printf("err: %v\n", err)
		os.Exit(1)
	}
	os.WriteFile("cam_snapshot.jpg", snap, 0644)
	fmt.Printf("Saved %d bytes\n", len(snap))
}
