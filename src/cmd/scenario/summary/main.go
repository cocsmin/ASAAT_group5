// Command summary prints min, median, mean, p95 and max of every metric per
// scenario from the CSV that "scenario experiment" writes, as a Markdown table.
//
//	go run ./cmd/scenario/summary docs/experiment-results.csv
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: summary results.csv")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil || len(rows) < 2 {
		fmt.Fprintln(os.Stderr, "cannot read rows:", err)
		os.Exit(1)
	}
	head := rows[0]
	metrics := []string{"detect_s", "recover_s", "outage_s", "flow_resume_s", "pans_lost"}
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	var order []string
	vals := map[string]map[string][]float64{}
	runs, pass := map[string]int{}, map[string]int{}
	for _, r := range rows[1:] {
		sc := r[col["scenario"]]
		if vals[sc] == nil {
			vals[sc] = map[string][]float64{}
			order = append(order, sc)
		}
		runs[sc]++
		if r[col["result"]] == "pass" {
			pass[sc]++
		}
		for _, m := range metrics {
			if v, err := strconv.ParseFloat(r[col[m]], 64); err == nil {
				vals[sc][m] = append(vals[sc][m], v)
			}
		}
	}
	fmt.Println("| scenario | runs | passed | metric | min | median | mean | p95 | max |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|")
	for _, sc := range order {
		for _, m := range metrics {
			v := vals[sc][m]
			if len(v) == 0 {
				continue
			}
			sort.Float64s(v)
			sum := 0.0
			for _, x := range v {
				sum += x
			}
			p95 := v[int(float64(len(v)-1)*0.95+0.5)]
			fmt.Printf("| %s | %d | %d | %s | %.2f | %.2f | %.2f | %.2f | %.2f |\n",
				sc, runs[sc], pass[sc], m, v[0], v[len(v)/2], sum/float64(len(v)), p95, v[len(v)-1])
		}
	}
}
