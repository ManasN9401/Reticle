package main

import (
	"encoding/json"
	"fmt"
	"github.com/hyperparallel/runtime/memory"
)

type TaskResponse struct {
	ID       string           `json:"id"`
	Artifact *memory.Artifact `json:"artifact,omitempty"`
}

func main() {
	line := `{"id": "exec-001|outline", "artifact": {"id": "readme_outline", "execution": "exec-001"}}`
	var resp TaskResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("Parsed: %+v\n", resp.Artifact)
	
	b, _ := json.Marshal(resp.Artifact)
	fmt.Printf("Marshaled: %s\n", string(b))
}
