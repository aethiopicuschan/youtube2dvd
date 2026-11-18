package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: validate-licenses <file>")
		os.Exit(2)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read file: %v\n", err)
		os.Exit(1)
	}

	var manifest struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			License string `json:"license"`
			Text    string `json:"text"`
		} `json:"packages"`
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		fmt.Fprintf(os.Stderr, "invalid JSON: %v\n", err)
		os.Exit(1)
	}

	for _, pkg := range manifest.Packages {
		if pkg.Name == "" || pkg.License == "" || pkg.Text == "" {
			fmt.Fprintf(
				os.Stderr,
				"incomplete license information: %s\n",
				pkg.Name,
			)
			os.Exit(1)
		}
	}
}
