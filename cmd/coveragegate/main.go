package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	profile := flag.String("profile", "", "Go coverage profile")
	minimum := flag.Float64("minimum", 100, "minimum statement coverage percentage")
	flag.Parse()
	if *profile == "" {
		fmt.Fprintln(os.Stderr, "-profile is required")
		os.Exit(2)
	}
	file, err := os.Open(*profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	total, covered := 0, 0
	line := 0
	for scanner.Scan() {
		line++
		text := scanner.Text()
		if line == 1 && strings.HasPrefix(text, "mode:") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			fmt.Fprintf(os.Stderr, "invalid coverage line %d\n", line)
			os.Exit(1)
		}
		statements, err1 := strconv.Atoi(fields[1])
		count, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || statements < 0 || count < 0 {
			fmt.Fprintf(os.Stderr, "invalid coverage data on line %d\n", line)
			os.Exit(1)
		}
		total += statements
		if count > 0 {
			covered += statements
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if total == 0 {
		fmt.Fprintln(os.Stderr, "coverage profile contains no statements")
		os.Exit(1)
	}
	actual := float64(covered) * 100 / float64(total)
	fmt.Printf("First-party statement coverage: %.2f%% (%d/%d statements)\n", actual, covered, total)
	if actual+1e-9 < *minimum {
		fmt.Fprintf(os.Stderr, "statement coverage %.2f%% is below required %.2f%%\n", actual, *minimum)
		os.Exit(1)
	}
}
